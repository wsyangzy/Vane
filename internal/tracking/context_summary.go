package tracking

import (
	"bytes"
	"encoding/json"
	"path"
	"regexp"
	"slices"
	"strings"
)

const (
	maxContextSummaryBytes = 8 << 20
	maxContextEntries      = 24
)

type contextPart struct {
	Tokens    int            `json:"tokens"`
	Items     int            `json:"items"`
	Breakdown []contextEntry `json:"breakdown,omitempty"`
}

type contextEntry struct {
	Name   string `json:"name,omitempty"`
	Kind   string `json:"kind"`
	Turn   int    `json:"turn,omitempty"`
	Tokens int    `json:"tokens"`
	Count  int    `json:"count"`
}

type contextRequestSummary struct {
	Version int                    `json:"version"`
	Parts   map[string]contextPart `json:"parts"`
	Partial bool                   `json:"partial,omitempty"`
	Turns   int                    `json:"turns,omitempty"`
}

type contextDocument struct {
	Type   string                 `json:"type"`
	Role   string                 `json:"role"`
	Text   string                 `json:"text"`
	Name   string                 `json:"name"`
	ID     string                 `json:"id"`
	CallID string                 `json:"call_id"`
	UseID  string                 `json:"tool_use_id"`
	File   string                 `json:"filename"`
	Args   json.RawMessage        `json:"arguments"`
	Input  json.RawMessage        `json:"input"`
	Body   json.RawMessage        `json:"content"`
	Tools  json.RawMessage        `json:"tools"`
	Func   *struct{ Name string } `json:"function"`
}

type contextBuilder struct {
	parts map[string]map[contextEntry]contextEntry
	calls map[string]contextEntry
	turn  int
}

var (
	contextName        = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]{0,79}$`)
	instructionHeading = regexp.MustCompile(`(?mi)^(?:# ([^ \t\r\n]+\.md)(?: instructions for [^\r\n]+)?|(?:Instructions from: |Contents of )([^\r\n]+?\.md)(?: \([^\r\n]+\):)?)[ \t\r]*$`)
	skillEntry         = regexp.MustCompile(`(?s)<skill>\s*<name>\s*([A-Za-z0-9_.-]{1,80})\s*</name>.*?</skill>`)
	skillListEntry     = regexp.MustCompile(`(?m)^\s*- ([A-Za-z0-9_][A-Za-z0-9_.-]{0,79}): [^\r\n]+`)
	contextSection     = regexp.MustCompile(`(?m)^<(skills_instructions|environment_context|user_instructions|project_instructions)>`)
)

// RecordContextSummary keeps structural labels and estimates. Request bodies,
// tool arguments and filesystem paths are never persisted in this summary.
func (ut *UsageTracker) RecordContextSummary(requestID, protocol string, body []byte) {
	if ut == nil || ut.config == nil || !ut.config.Enabled || !ut.hotPoolEnabled || ut.hotPool == nil || requestID == "" {
		return
	}
	summary := buildContextSummary(protocol, body)
	if summary == nil {
		return
	}
	var estimate int64
	for _, part := range summary.Parts {
		estimate += int64(part.Tokens)
	}
	data, _ := json.Marshal(summary)
	encoded := string(data)
	_ = ut.hotPool.Update(requestID, func(req *ActiveRequest) {
		req.ContextSummary = encoded
		req.contextInputEstimate = estimate
	})
}

func buildContextSummary(protocol string, body []byte) *contextRequestSummary {
	if len(body) == 0 || len(body) > maxContextSummaryBytes {
		return nil
	}
	var payload struct {
		System       json.RawMessage
		Instructions json.RawMessage
		Tools        []json.RawMessage
		Input        json.RawMessage
		Messages     json.RawMessage
		Previous     json.RawMessage `json:"previous_response_id"`
		Conversation json.RawMessage
	}
	if json.Unmarshal(body, &payload) != nil {
		return nil
	}

	builder := contextBuilder{parts: make(map[string]map[contextEntry]contextEntry), calls: make(map[string]contextEntry)}
	input, system := payload.Input, payload.Instructions
	if protocol == "messages" {
		input, system = payload.Messages, payload.System
	}
	contextBlocks(system, func(block json.RawMessage) {
		builder.addText(contextText(block), "system", "prompt")
	})
	for _, tool := range payload.Tools {
		builder.addTool(tool, true)
	}
	contextBlocks(input, builder.addInput)
	partial := hasContextValue(payload.Previous) || hasContextValue(payload.Conversation)
	if len(builder.parts) == 0 && !partial {
		return nil
	}
	return builder.summary(partial)
}

func hasContextValue(raw json.RawMessage) bool {
	return len(raw) > 0 && !bytes.Equal(raw, []byte("null")) && !bytes.Equal(raw, []byte(`""`))
}

func contextBlocks(raw json.RawMessage, visit func(json.RawMessage)) {
	if !hasContextValue(raw) {
		return
	}
	if raw[0] != '[' {
		visit(raw)
		return
	}
	var blocks []json.RawMessage
	if json.Unmarshal(raw, &blocks) == nil {
		for _, block := range blocks {
			visit(block)
		}
	}
}

func contextText(raw json.RawMessage) string {
	var text string
	if len(raw) > 0 && raw[0] == '"' {
		_ = json.Unmarshal(raw, &text)
		return text
	}
	var block struct{ Text string }
	_ = json.Unmarshal(raw, &block)
	return block.Text
}

func contextLabel(value string) string {
	if !contextName.MatchString(value) {
		return ""
	}
	lower := strings.ToLower(value)
	for _, prefix := range []string{"sk-", "sk_", "rk-", "ghp_", "github_pat_", "akia"} {
		if strings.HasPrefix(lower, prefix) {
			return ""
		}
	}
	return value
}

func contextFilename(value string) string {
	return contextLabel(path.Base(strings.ReplaceAll(strings.TrimSpace(value), `\`, "/")))
}

func (b *contextBuilder) add(group string, entry contextEntry, size, count int) {
	if size <= 0 {
		return
	}
	entries := b.parts[group]
	if entries == nil {
		entries = make(map[contextEntry]contextEntry)
		b.parts[group] = entries
	}
	total, exists := entries[entry]
	if !exists {
		total = entry
	}
	total.Tokens += (size + 3) / 4
	total.Count += max(count, 1)
	entries[entry] = total
}

func (b *contextBuilder) addText(text, group, kind string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	if group == "chat" && !isContextMetadata(text) {
		b.addChat(len(text))
		return
	}
	start := 0
	for _, section := range contextSection.FindAllStringIndex(text, -1) {
		if section[0] > start {
			b.addTextSection(strings.TrimSpace(text[start:section[0]]), group, kind)
		}
		start = section[0]
	}
	b.addTextSection(text[start:], group, kind)
}

func (b *contextBuilder) addTextSection(text, group, kind string) {
	if matches := instructionHeading.FindAllStringSubmatchIndex(text, -1); len(matches) > 0 {
		b.addTextSection(strings.TrimSpace(text[:matches[0][0]]), group, kind)
		for index, match := range matches {
			start, stop := match[2], match[3]
			if start < 0 {
				start, stop = match[4], match[5]
			}
			end := len(text)
			if index+1 < len(matches) {
				end = matches[index+1][0]
			}
			b.add("memory", contextEntry{Kind: "instruction", Name: contextFilename(text[start:stop])}, end-match[0], 1)
		}
		return
	}
	switch {
	case strings.HasPrefix(text, "<skills_instructions>"), strings.HasPrefix(text, "<available_skills>"),
		strings.HasPrefix(text, "The following skills are available for use with the Skill tool:"):
		b.addSkills(text)
	case strings.HasPrefix(text, "<project_instructions>"), strings.HasPrefix(text, "<system-reminder>"):
		b.add("memory", contextEntry{Kind: "instruction"}, len(text), 1)
	case strings.HasPrefix(text, "<environment_context>"):
		b.add("system", contextEntry{Kind: "environment"}, len(text), 1)
	case strings.HasPrefix(text, "<user_instructions>"):
		b.add("system", contextEntry{Kind: "user_instructions"}, len(text), 1)
	case group == "chat":
		b.addChat(len(text))
	default:
		b.add(group, contextEntry{Kind: kind}, len(text), 1)
	}
}

func (b *contextBuilder) addSkills(text string) {
	remaining := len(text)
	matches := skillEntry.FindAllStringSubmatchIndex(text, -1)
	if len(matches) == 0 {
		entries := text
		if _, list, ok := strings.Cut(text, "<available_skills>"); ok {
			entries, _, _ = strings.Cut(list, "</available_skills>")
		}
		matches = skillListEntry.FindAllStringSubmatchIndex(entries, -1)
		text = entries
	}
	for _, match := range matches {
		size := match[1] - match[0]
		b.add("skills", contextEntry{Kind: "skill", Name: contextLabel(text[match[2]:match[3]])}, size, 1)
		remaining -= size
	}
	b.add("skills", contextEntry{Kind: "skill"}, remaining, 1)
}

func (b *contextBuilder) addTool(raw json.RawMessage, expandFunctions bool) {
	var tool contextDocument
	if json.Unmarshal(raw, &tool) != nil {
		return
	}
	name := tool.Name
	if name == "" && tool.Func != nil {
		name = tool.Func.Name
	}
	if name == "" {
		name = tool.Type
	}
	kind, count := "tool", 1
	if tool.Type == "namespace" || hasContextValue(tool.Tools) {
		var children []json.RawMessage
		_ = json.Unmarshal(tool.Tools, &children)
		if name == "functions" && expandFunctions && len(children) > 0 {
			for _, child := range children {
				b.addTool(child, false)
			}
			return
		}
		kind, count = "namespace", len(children)
	}
	if server, ok := strings.CutPrefix(name, "mcp__"); ok {
		name, _, _ = strings.Cut(server, "__")
		kind = "mcp"
	}
	b.add("tools", contextEntry{Kind: kind, Name: contextLabel(name)}, len(raw), count)
}

func isContextMetadata(text string) bool {
	for _, prefix := range []string{"<environment_context>", "<system-reminder>", "<project_instructions>", "<skills_instructions>", "<user_instructions>", "<available_skills>"} {
		if strings.HasPrefix(strings.TrimSpace(text), prefix) {
			return true
		}
	}
	prefix, _, _ := strings.Cut(strings.TrimSpace(text), "\n")
	return strings.HasPrefix(prefix, "# ") && strings.Contains(prefix, ".md instructions for ")
}

func (b *contextBuilder) addChat(size int) {
	if b.turn == 0 {
		b.add("chat", contextEntry{Kind: "conversation"}, size, 1)
		return
	}
	b.add("chat", contextEntry{Kind: "turn", Turn: b.turn}, size, 1)
}

func (b *contextBuilder) rememberCall(id, name string, input json.RawMessage) {
	if id == "" {
		return
	}
	entry := contextEntry{Kind: "result", Name: contextLabel(name)}
	tool := strings.ToLower(name)
	if tool == "read" || tool == "read_file" || strings.HasSuffix(tool, "__read_file") {
		if len(input) > 0 && input[0] == '"' {
			var decoded string
			_ = json.Unmarshal(input, &decoded)
			input = json.RawMessage(decoded)
		}
		var args struct {
			Path     string `json:"path"`
			FilePath string `json:"file_path"`
		}
		if json.Unmarshal(input, &args) == nil {
			filename := contextFilename(args.Path)
			if filename == "" {
				filename = contextFilename(args.FilePath)
			}
			if filename != "" && !strings.HasPrefix(filename, ".") {
				entry = contextEntry{Kind: "file", Name: filename}
			}
		}
	}
	b.calls[id] = entry
}

func (b *contextBuilder) addResult(id string, size int) {
	entry, found := b.calls[id]
	if !found {
		entry = contextEntry{Kind: "result"}
	}
	group := "results"
	if entry.Kind == "file" {
		group = "files"
	}
	b.add(group, entry, size, 1)
}

func (b *contextBuilder) addInput(raw json.RawMessage) {
	if len(raw) == 0 {
		return
	}
	if raw[0] == '"' {
		b.addMessage("user", raw)
		return
	}
	var item contextDocument
	if json.Unmarshal(raw, &item) != nil {
		return
	}
	switch {
	case item.Role == "system" || item.Role == "developer":
		contextBlocks(item.Body, func(block json.RawMessage) {
			b.addText(contextText(block), "system", item.Role)
		})
	case item.Type == "additional_tools":
		contextBlocks(item.Tools, func(tool json.RawMessage) { b.addTool(tool, true) })
	case item.Type == "compaction" || item.Type == "compaction_summary":
		b.add("summary", contextEntry{Kind: "compaction"}, len(raw), 1)
	case item.Type == "tool_search_output":
		contextBlocks(item.Tools, func(tool json.RawMessage) { b.addTool(tool, true) })
		if !hasContextValue(item.Tools) {
			b.addResult(item.CallID, len(raw))
		}
	case item.Role == "tool" || strings.HasSuffix(item.Type, "_call_output"):
		b.addResult(item.CallID, len(raw))
	case strings.HasSuffix(item.Type, "_call"):
		b.rememberCall(item.CallID, item.Name, item.Args)
		b.addChat(len(raw))
	case hasContextValue(item.Body):
		b.addMessage(item.Role, item.Body)
	default:
		b.addChat(len(raw))
	}
}

func (b *contextBuilder) addMessage(role string, raw json.RawMessage) {
	if len(raw) > 1 && raw[0] == '"' {
		text := bytes.TrimSpace(raw[1 : len(raw)-1])
		if len(text) == 0 {
			return
		}
		// Plain conversation only needs a length. Escape-prefixed strings may
		// encode a metadata opener (for example, JSON escaped "<").
		if text[0] != '<' && text[0] != '#' && text[0] != '\\' {
			if role == "user" {
				b.turn++
			}
			b.addChat(len(text))
			return
		}
	}
	var blocks []json.RawMessage
	contextBlocks(raw, func(block json.RawMessage) { blocks = append(blocks, block) })
	content := make([]contextDocument, len(blocks))
	for index, block := range blocks {
		if len(block) > 0 && block[0] == '"' {
			content[index].Text = contextText(block)
		} else {
			_ = json.Unmarshal(block, &content[index])
		}
	}
	if role == "user" && slices.ContainsFunc(content, func(block contextDocument) bool {
		switch block.Type {
		case "image", "input_image", "document", "input_file":
			return true
		default:
			return strings.TrimSpace(block.Text) != "" && !isContextMetadata(block.Text)
		}
	}) {
		b.turn++
	}
	for index, block := range content {
		switch block.Type {
		case "tool_use", "server_tool_use":
			b.rememberCall(block.ID, block.Name, block.Input)
			b.addChat(len(blocks[index]))
		case "tool_result":
			b.addResult(block.UseID, len(blocks[index]))
		case "image", "input_image":
			b.add("files", contextEntry{Kind: "image"}, 4*1600, 1)
		case "document", "input_file":
			b.add("files", contextEntry{Kind: "file", Name: contextFilename(block.File)}, len(blocks[index]), 1)
		default:
			if block.Text != "" {
				b.addText(block.Text, "chat", "turn")
			} else {
				b.addChat(len(blocks[index]))
			}
		}
	}
}

func (b *contextBuilder) summary(partial bool) *contextRequestSummary {
	parts := make(map[string]contextPart, len(b.parts))
	for group, entries := range b.parts {
		part := contextPart{Breakdown: make([]contextEntry, 0, len(entries))}
		for _, entry := range entries {
			part.Tokens += entry.Tokens
			part.Items += entry.Count
			part.Breakdown = append(part.Breakdown, entry)
		}
		slices.SortFunc(part.Breakdown, func(a, b contextEntry) int {
			if group == "chat" {
				return b.Turn - a.Turn
			}
			if a.Tokens != b.Tokens {
				return b.Tokens - a.Tokens
			}
			return strings.Compare(a.Kind+"."+a.Name, b.Kind+"."+b.Name)
		})
		if len(part.Breakdown) > maxContextEntries {
			combined := contextEntry{Kind: "other"}
			for _, entry := range part.Breakdown[maxContextEntries-1:] {
				combined.Tokens += entry.Tokens
				combined.Count += entry.Count
			}
			part.Breakdown = append(part.Breakdown[:maxContextEntries-1], combined)
		}
		parts[group] = part
	}
	return &contextRequestSummary{Version: 2, Parts: parts, Partial: partial, Turns: b.turn}
}
