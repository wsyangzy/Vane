package sessionusage

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"

	"github.com/wsyangzy/Vane/internal/privacy"
)

const titleLimit = 160

var titlePrivateKeyStart = regexp.MustCompile(`-----BEGIN (?:[A-Z0-9]+ )*PRIVATE KEY(?: BLOCK)?-----`)

var titlePrivacy = func() *privacy.Snapshot {
	var rules []privacy.Rule
	for _, preset := range privacy.Presets() {
		rules = append(rules, preset.Rules...)
	}
	compiled, err := privacy.CompileRules(rules)
	if err != nil {
		return nil
	}
	return &privacy.Snapshot{Rules: compiled}
}()

// Keep a small, sanitized display label, never the full prompt or message body.
// Scan before display truncation so cutting a long key cannot bypass its matcher.
func displayTitle(value string) string {
	if len(value) > 4096 {
		value = strings.ToValidUTF8(value[:4096], "")
	}
	// The bounded scan may omit a PEM closing marker. Discard everything from
	// its opening marker rather than retaining a partial private key as a title.
	if marker := titlePrivateKeyStart.FindStringIndex(value); marker != nil {
		value = value[:marker[0]] + "[私钥]"
	}
	if titlePrivacy == nil {
		return ""
	}
	value = string(titlePrivacy.ApplyToText(value).Body)
	var text strings.Builder
	space, count := false, 0
	for _, r := range value {
		if unicode.IsSpace(r) {
			space = text.Len() > 0
			continue
		}
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			continue
		}
		if count >= titleLimit {
			break
		}
		if space {
			text.WriteByte(' ')
			space = false
			count++
			if count >= titleLimit {
				break
			}
		}
		text.WriteRune(r)
		count++
	}
	return strings.TrimSpace(text.String())
}

func promptTitle(value string) string {
	value = strings.TrimSpace(value)
	// Native environment/instruction messages and tool envelopes are not topics.
	if strings.HasPrefix(value, "<") || strings.HasPrefix(value, "# AGENTS.md") {
		return ""
	}
	return displayTitle(value)
}

func textContent(raw json.RawMessage) string {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text
	}
	var blocks []struct{ Type, Text string }
	if json.Unmarshal(raw, &blocks) != nil {
		return ""
	}
	for _, block := range blocks {
		if block.Type == "text" || block.Type == "input_text" {
			if title := promptTitle(block.Text); title != "" {
				return title
			}
		}
	}
	return ""
}

func (p *parser) name(value string, priority int) {
	if priority < p.titlePriority || priority == p.titlePriority && priority == 1 && p.title != "" {
		return
	}
	if title := displayTitle(value); title != "" {
		p.title, p.titlePriority = title, priority
	}
}

func (p *parser) firstPrompt(line []byte, provider string) {
	if p.title != "" {
		return
	}
	var event struct {
		Message struct{ Content json.RawMessage }
		Payload struct {
			Message string
			Content json.RawMessage
		}
	}
	if json.Unmarshal(line, &event) != nil {
		return
	}
	text := textContent(event.Message.Content)
	if provider == "codex" {
		text = event.Payload.Message
		if text == "" {
			text = textContent(event.Payload.Content)
		}
	}
	if title := promptTitle(text); title != "" {
		p.name(title, 1)
	}
}

type titleIndex struct {
	path   string
	info   os.FileInfo
	offset int64
	names  map[string]string
}

// Codex keeps renames in a small append-only index independent of transcripts.
// Read once per snapshot, not once per session; incomplete final lines wait.
func (r *Reader) codexTitles(ctx context.Context, root string) map[string]string {
	if root == "" {
		return nil
	}
	path := filepath.Join(root, "session_index.jsonl")
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		r.titles = titleIndex{}
		return nil
	}
	index := &r.titles
	if index.path == path && index.info != nil && os.SameFile(index.info, info) && info.Size() == index.info.Size() && info.ModTime().Equal(index.info.ModTime()) {
		r.noteFile(true)
		return index.names
	}
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	if index.path != path || index.info == nil || !os.SameFile(index.info, info) || info.Size() <= index.info.Size() {
		*index = titleIndex{path: path, names: make(map[string]string)}
	}
	if _, err := f.Seek(index.offset, io.SeekStart); err != nil {
		return nil
	}
	r.noteFile(false)
	scan := bufio.NewScanner(f)
	scan.Buffer(make([]byte, 4096), 1<<20)
	scan.Split(func(data []byte, _ bool) (int, []byte, error) {
		if i := bytes.IndexByte(data, '\n'); i >= 0 {
			index.offset += int64(i + 1)
			return i + 1, data[:i], nil
		}
		return 0, nil, nil
	})
	for scan.Scan() {
		if ctx.Err() != nil {
			index.info = nil
			return nil
		}
		var row struct {
			ID   string `json:"id"`
			Name string `json:"thread_name"`
		}
		if json.Unmarshal(scan.Bytes(), &row) != nil || row.ID == "" {
			continue
		}
		if name := displayTitle(row.Name); name != "" {
			index.names[row.ID] = name
		}
	}
	if scan.Err() != nil {
		index.info = nil
		return nil
	}
	index.info = info
	return index.names
}
