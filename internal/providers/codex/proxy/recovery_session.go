package codexproxy

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var recoverySessionID = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// Read only the explicitly identified local session. Native history is not an
// issuer authority: each returned value must match Vane's protected digest.
func readNativeReasoning(ctx context.Context, session string, needed map[string]string) map[string]json.RawMessage {
	if !recoverySessionID.MatchString(session) || len(needed) == 0 {
		return nil
	}
	dir := os.Getenv("CODEX_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil
		}
		dir = filepath.Join(home, ".codex")
	}
	return readNativeReasoningAt(ctx, dir, session, needed)
}

func readNativeReasoningAt(ctx context.Context, directory, session string, needed map[string]string) map[string]json.RawMessage {
	if !recoverySessionID.MatchString(session) || len(needed) == 0 {
		return nil
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return nil
	}
	defer root.Close()
	var found string
	visited := 0
	for _, folder := range []string{"sessions", "archived_sessions"} {
		err = fs.WalkDir(root.FS(), folder, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				if path == folder && os.IsNotExist(walkErr) {
					return fs.SkipDir
				}
				return walkErr
			}
			visited++
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if visited > 20000 {
				return fs.ErrInvalid
			}
			if entry.Type()&os.ModeSymlink != 0 {
				return nil
			}
			if entry.IsDir() || !strings.HasPrefix(entry.Name(), "rollout-") || !strings.HasSuffix(strings.ToLower(entry.Name()), strings.ToLower(session)+".jsonl") {
				return nil
			}
			if found != "" {
				return fs.ErrInvalid
			}
			found = path
			return nil
		})
		if err != nil && !os.IsNotExist(err) {
			return nil
		}
	}
	if found == "" {
		return nil
	}
	file, err := root.Open(found)
	if err != nil {
		return nil
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > 64<<20 {
		return nil
	}
	scanner := bufio.NewScanner(io.LimitReader(file, info.Size()))
	scanner.Buffer(make([]byte, 64<<10), maxCodexStreamEventBytes+1)
	result := make(map[string]json.RawMessage)
	retained := 0
	confirmed := false
	for scanner.Scan() {
		if ctx.Err() != nil {
			return nil
		}
		var event struct {
			Type    string
			Payload json.RawMessage
		}
		if json.Unmarshal(scanner.Bytes(), &event) != nil {
			return nil
		}
		if event.Type == "session_meta" {
			var meta struct{ ID string }
			if confirmed || json.Unmarshal(event.Payload, &meta) != nil || !strings.EqualFold(meta.ID, session) {
				return nil
			}
			confirmed = true
			continue
		}
		if !confirmed {
			return nil
		}
		if event.Type == "compacted" {
			clear(result)
			retained = 0
			continue
		}
		if event.Type == "event_msg" {
			var msg struct{ Type string }
			_ = json.Unmarshal(event.Payload, &msg)
			if msg.Type == "thread_rolled_back" {
				clear(result)
				retained = 0
			}
			continue
		}
		if event.Type != "response_item" {
			continue
		}
		raw := event.Payload
		var item struct {
			ID, Type, Status string
			EncryptedContent string `json:"encrypted_content"`
		}
		if json.Unmarshal(raw, &item) != nil {
			return nil
		}
		if item.Type == "compaction" || item.Type == "context_compaction" {
			clear(result)
			retained = 0
			continue
		}
		if item.Type != "reasoning" || item.ID == "" || item.EncryptedContent == "" || item.Status != "" && item.Status != "completed" {
			continue
		}
		id := reasoningHash(item.ID)
		if expected, ok := needed[id]; ok && reasoningHash(item.EncryptedContent) == expected {
			retained += len(raw) - len(result[id])
			if retained > maxCompletedReasoningBytes {
				return nil
			}
			if len(result) < 256 {
				result[id] = append(json.RawMessage(nil), raw...)
			}
		}
	}
	if scanner.Err() != nil || !confirmed {
		return nil
	}
	return result
}
