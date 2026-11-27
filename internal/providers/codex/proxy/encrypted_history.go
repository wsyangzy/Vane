package codexproxy

import (
	"bytes"
	"encoding/json"
	"io"
)

// withoutEncryptedReasoning is the explicit history-recovery exception to
// request fidelity. It removes only reasoning items carrying nonempty sealed
// state. Retained items and all bytes outside input remain byte-for-byte intact.
// Server-side continuation and compaction cannot be reconstructed here.
func withoutEncryptedReasoning(body []byte) ([]byte, int) {
	start, end, input, ok := encryptedHistoryInput(body)
	if !ok {
		return body, 0
	}
	kept := make([][]byte, 0, len(input))
	removed := 0
	for _, raw := range input {
		var item struct {
			Type             string `json:"type"`
			EncryptedContent string `json:"encrypted_content"`
		}
		if json.Unmarshal(raw, &item) != nil || item.Type == "compaction" || item.Type == "context_compaction" {
			return body, 0
		}
		if item.Type == "reasoning" && item.EncryptedContent != "" {
			removed++
			continue
		}
		kept = append(kept, raw)
	}
	if removed == 0 || len(kept) == 0 {
		return body, 0
	}
	out := make([]byte, 0, len(body))
	out = append(out, body[:start]...)
	out = append(out, '[')
	out = append(out, bytes.Join(kept, []byte(","))...)
	out = append(out, ']')
	out = append(out, body[end:]...)
	return out, removed
}

// Return the original input span; callers must preserve all other fields.
func encryptedHistoryInput(body []byte) (start, end int, input []json.RawMessage, ok bool) {
	dec := json.NewDecoder(bytes.NewReader(body))
	first, err := dec.Token()
	if err != nil || first != json.Delim('{') {
		return
	}
	for dec.More() {
		key, err := dec.Token()
		if err != nil {
			return
		}
		var raw json.RawMessage
		if dec.Decode(&raw) != nil {
			return
		}
		switch key {
		case "previous_response_id", "conversation":
			if string(raw) != "null" && string(raw) != `""` {
				return
			}
		case "input":
			if end != 0 || len(raw) == 0 || raw[0] != '[' || json.Unmarshal(raw, &input) != nil {
				return
			}
			end = int(dec.InputOffset())
			start = end - len(raw)
		}
	}
	if _, err := dec.Token(); err != nil {
		return
	}
	if _, err := dec.Token(); err != io.EOF || end == 0 {
		return
	}
	return start, end, input, true
}
