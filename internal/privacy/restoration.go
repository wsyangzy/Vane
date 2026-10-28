package privacy

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"sync"
	"time"
)

const restorationTTL = time.Hour
const maxRestorationSessions = 128
const maxRestorationValues = 512
const maxRestorationBytes = 128 * 1024

type restoredValue struct{ raw, token, kind string }
type restorationSession struct {
	mu       sync.Mutex
	values   []restoredValue
	bytes    int
	lastUsed time.Time
}
type restorationVault struct {
	mu       sync.Mutex
	key      [32]byte
	sessions map[string]*restorationSession
	now      func() time.Time
}

func newRestorationVault() *restorationVault {
	v := &restorationVault{sessions: make(map[string]*restorationSession), now: time.Now}
	if _, err := rand.Read(v.key[:]); err != nil {
		panic("privacy: secure random unavailable")
	}
	return v
}

// Restoration is request-local; plaintext mappings never cross the desktop API
// or storage boundary. The shared cache is bounded to 16 MiB and one hour idle.
type Restoration struct {
	mu       sync.Mutex
	session  *restorationSession
	key      [32]byte
	values   []restoredValue
	active   bool
	restored int
	skipped  string
	activity *activityHandle
}

func (v *restorationVault) request(scope, requestID string) *Restoration {
	v.mu.Lock()
	defer v.mu.Unlock()
	now := v.now()
	for k, s := range v.sessions {
		if now.Sub(s.lastUsed) >= restorationTTL {
			delete(v.sessions, k)
		}
	}
	// Missing explicit conversation identity must never create a global session.
	requestOnly := scope == ""
	if requestOnly {
		var nonce [32]byte
		if _, err := rand.Read(nonce[:]); err != nil {
			panic("privacy: secure random unavailable")
		}
		scope = requestID + hex.EncodeToString(nonce[:])
	}
	h := hmac.New(sha256.New, v.key[:])
	h.Write([]byte(scope))
	var key [32]byte
	copy(key[:], h.Sum(nil))
	if requestOnly {
		return &Restoration{session: &restorationSession{}, key: key}
	}
	cacheKey := hex.EncodeToString(key[:])
	s := v.sessions[cacheKey]
	if s == nil {
		if len(v.sessions) >= maxRestorationSessions {
			oldestKey := ""
			var oldest time.Time
			for k, entry := range v.sessions {
				if oldestKey == "" || entry.lastUsed.Before(oldest) {
					oldestKey, oldest = k, entry.lastUsed
				}
			}
			delete(v.sessions, oldestKey)
		}
		s = &restorationSession{}
		v.sessions[cacheKey] = s
	}
	s.lastUsed = now
	s.mu.Lock()
	values := append([]restoredValue(nil), s.values...)
	s.mu.Unlock()
	return &Restoration{session: s, key: key, values: values}
}

func (r *Restoration) Replace(kind, raw string, shape bool) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, value := range r.values {
		if value.raw == raw {
			return value.token
		}
	}
	r.session.mu.Lock()
	defer r.session.mu.Unlock()
	// Concurrent requests in the same conversation share the same replacement.
	for _, value := range r.session.values {
		if value.raw == raw {
			r.values = append(r.values, value)
			return value.token
		}
	}
	if len(raw) > 16*1024 || len(r.session.values) >= maxRestorationValues || r.session.bytes+2*len(raw)+64 > maxRestorationBytes {
		r.skipped = "mapping_capacity"
		return DefaultPlaceholder
	}
	h := hmac.New(sha256.New, r.key[:])
	h.Write([]byte(raw))
	sum := h.Sum(nil)
	token := "__VANE_" + strings.ToUpper(kind) + "_" + hex.EncodeToString(sum[:12]) + "__"
	if shape && (kind == "api_key" || kind == "token" || kind == "secret" || kind == "password") {
		if shaped := shapeSecret(raw, sum); shaped != "" {
			token = shaped
		}
	}
	value := restoredValue{raw: raw, token: token, kind: kind}
	r.values = append(r.values, value)
	r.session.values = append(r.session.values, value)
	r.session.bytes += len(raw) + len(token)
	return token
}

// Keep only an identifying prefix, separators and character classes. Never use
// the real credential as the replacement or preserve its payload characters.
func shapeSecret(raw string, seed []byte) string {
	if len(raw) < 12 || len(raw) > 512 {
		return ""
	}
	out := []byte(raw)
	prefix := 0
	if i := strings.IndexAny(raw, "_-"); i >= 2 && i <= 12 {
		prefix = i + 1
	}
	changed := 0
	for i := prefix; i < len(out); i++ {
		n := seed[i%len(seed)]
		switch c := out[i]; {
		case c >= 'a' && c <= 'z':
			out[i] = 'a' + n%26
			changed++
		case c >= 'A' && c <= 'Z':
			out[i] = 'A' + n%26
			changed++
		case c >= '0' && c <= '9':
			out[i] = '0' + n%10
			changed++
		case c == '_' || c == '-' || c == '.':
		default:
			return ""
		}
	}
	if changed < 8 || string(out) == raw {
		return ""
	}
	return string(out)
}

func (r *Restoration) knownMatches(kind, text string) [][2]int {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out [][2]int
	for _, value := range r.values {
		if value.kind != kind {
			continue
		}
		for offset := 0; offset < len(text); {
			i := strings.Index(text[offset:], value.raw)
			if i < 0 {
				break
			}
			start := offset + i
			out = append(out, [2]int{start, start + len(value.raw)})
			offset = start + len(value.raw)
		}
	}
	return out
}
func (r *Restoration) isReplacement(value string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, item := range r.values {
		if value == item.token {
			return true
		}
	}
	return false
}
func (r *Restoration) enable() {
	if r != nil {
		r.mu.Lock()
		r.active = true
		r.mu.Unlock()
	}
}

// restoreText buffers only a suffix that can still become a replacement. The
// caller supplies a separate tail for every text/tool stream to prevent mixing.
func (r *Restoration) restoreText(text string, innerJSON bool, final bool) (string, string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.active {
		return text, ""
	}
	var out strings.Builder
	for len(text) > 0 {
		at, which := -1, -1
		for i, item := range r.values {
			if j := strings.Index(text, item.token); j >= 0 && (at < 0 || j < at) {
				at, which = j, i
			}
		}
		if at < 0 {
			break
		}
		out.WriteString(text[:at])
		raw := r.values[which].raw
		if innerJSON {
			raw = string(encodeJSONString(raw))
			raw = raw[1 : len(raw)-1]
		}
		out.WriteString(raw)
		text = text[at+len(r.values[which].token):]
		r.restored++
	}
	tail := ""
	if !final {
		longest := 0
		for _, item := range r.values {
			max := min(len(text), len(item.token)-1)
			for n := max; n > longest; n-- {
				if strings.HasSuffix(text, item.token[:n]) {
					longest = n
					break
				}
			}
		}
		if longest > 0 {
			tail = text[len(text)-longest:]
			text = text[:len(text)-longest]
		}
	}
	out.WriteString(text)
	return out.String(), tail
}
