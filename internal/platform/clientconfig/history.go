package clientconfig

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const historySuffix = ".vane-config.dpapi"

// History contains only Vane-created snapshots. Existing user backups are never
// imported or overwritten. Metadata and file contents are encrypted together.
type History struct {
	Directory string
	Vault     Vault
}

type Record struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Directory  string    `json:"directory"`
	CreatedAt  time.Time `json:"created_at"`
	Provider   string    `json:"provider"`
	Operation  string    `json:"operation"`
	ConfigPath string    `json:"config_path"`
	Exists     bool      `json:"exists"`
	Size       int       `json:"size"`
}

func validRecordID(id string) bool {
	if len(id) != 32 {
		return false
	}
	_, err := hex.DecodeString(id)
	return err == nil
}

func (h History) recordDirectory(record Record) string {
	stamp := record.CreatedAt.In(time.FixedZone("CST", 8*60*60)).Format("20060102-150405")
	return filepath.Join(h.Directory, stamp+"_"+record.Provider+"_"+record.ID)
}

func defaultRecordName(record Record) string {
	provider := "Codex"
	if record.Provider == "claude" {
		provider = "Claude Code"
	}
	return provider + " · " + record.CreatedAt.In(time.FixedZone("CST", 8*60*60)).Format("2006-01-02 15:04:05")
}

func normalizeRecordName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if utf8.RuneCountInString(name) > 80 || strings.IndexFunc(name, unicode.IsControl) >= 0 {
		return "", errors.New("备份名称最多 80 个字符，不能包含换行或控制字符")
	}
	return name, nil
}

// The record ID remains stable when a user edits its display name. Directory
// names contain the local timestamp and provider; labels are metadata only.
func (h History) snapshotPath(id string) (string, error) {
	if !validRecordID(id) {
		return "", errors.New("备份记录无效")
	}
	entries, err := os.ReadDir(h.Directory)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", errors.New("无法读取备份目录")
	}
	for _, entry := range entries {
		if entry.IsDir() && strings.HasSuffix(entry.Name(), "_"+id) {
			path := filepath.Join(h.Directory, entry.Name(), "config.dpapi")
			if _, err := os.Lstat(path); err == nil {
				return path, nil
			}
		}
	}
	return filepath.Join(h.Directory, id+historySuffix), nil
}

type historySnapshot struct {
	Version  int
	Record   Record
	Snapshot Snapshot
	Revision string
}

func (h History) load(id string) (historySnapshot, error) {
	var b historySnapshot
	path, err := h.snapshotPath(id)
	if err != nil {
		return b, err
	}
	s, err := read(path, maxSize*2)
	if err != nil || !s.Exists {
		return b, errors.New("备份文件不可读或不存在")
	}
	plain, err := h.Vault.Open(s.Data)
	if err != nil {
		return b, errors.New("无法解密配置备份，请使用创建备份的 Windows 用户")
	}
	defer clear(plain)
	if json.Unmarshal(plain, &b) != nil || b.Version != 1 || b.Record.ID != id ||
		b.Revision != b.Snapshot.Revision() || b.Record.Exists != b.Snapshot.Exists ||
		b.Record.Size != len(b.Snapshot.Data) || len(b.Snapshot.Data) > maxSize ||
		!filepath.IsAbs(b.Record.ConfigPath) || (b.Record.Provider != "claude" && b.Record.Provider != "codex") {
		return historySnapshot{}, errors.New("配置备份无效或内容已损坏")
	}
	b.Record.Directory = filepath.Dir(path)
	if b.Record.Directory == h.Directory {
		b.Record.Directory = h.recordDirectory(b.Record)
	}
	b.Record.Name = defaultRecordName(b.Record)
	metadata, err := read(filepath.Join(b.Record.Directory, "record.json"), 2048)
	if err != nil {
		return historySnapshot{}, err
	}
	if metadata.Exists {
		var label struct {
			Name string `json:"name"`
		}
		if json.Unmarshal(metadata.Data, &label) != nil {
			return historySnapshot{}, errors.New("备份名称记录无效")
		}
		name, err := normalizeRecordName(label.Name)
		if err != nil {
			return historySnapshot{}, err
		}
		if name != "" {
			b.Record.Name = name
		}
	}
	return b, nil
}

func (h History) organize(record Record) error {
	legacy := filepath.Join(h.Directory, record.ID+historySuffix)
	old, err := read(legacy, maxSize*2)
	if err != nil || !old.Exists {
		return err
	}
	if err := os.MkdirAll(record.Directory, 0700); err != nil {
		return errors.New("创建备份记录目录失败")
	}
	target := filepath.Join(record.Directory, "config.dpapi")
	existing, err := read(target, maxSize*2)
	if err != nil {
		return err
	}
	if existing.Exists {
		if !bytes.Equal(existing.Data, old.Data) {
			return errors.New("备份记录目录已存在不同内容，原备份已保留")
		}
		return os.Remove(legacy)
	}
	// Rename preserves the encrypted bytes and original backup timestamp. The
	// lock serializes migration with saves and restores within this application.
	if err := os.Rename(legacy, target); err != nil {
		return errors.New("整理备份记录目录失败，原备份已保留")
	}
	return nil
}

func (h History) list(provider, path string) ([]Record, error) {
	entries, err := os.ReadDir(h.Directory)
	if errors.Is(err, os.ErrNotExist) {
		return []Record{}, nil
	}
	if err != nil {
		return nil, errors.New("无法读取备份目录")
	}
	result := []Record{}
	seen := map[string]bool{}
	for _, e := range entries {
		id := ""
		if e.IsDir() {
			parts := strings.Split(e.Name(), "_")
			id = parts[len(parts)-1]
		} else if strings.HasSuffix(e.Name(), historySuffix) {
			id = strings.TrimSuffix(e.Name(), historySuffix)
		}
		if !validRecordID(id) || seen[id] {
			continue
		}
		seen[id] = true
		b, err := h.load(id)
		if err != nil {
			return nil, err
		}
		if err := h.organize(b.Record); err != nil {
			clear(b.Snapshot.Data)
			return nil, err
		}
		if (provider == "" || b.Record.Provider == provider) && (path == "" || filepath.Clean(b.Record.ConfigPath) == filepath.Clean(path)) {
			result = append(result, b.Record)
		}
		clear(b.Snapshot.Data)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].CreatedAt.After(result[j].CreatedAt) })
	return result, nil
}

func (h History) List(provider, path string) ([]Record, error) {
	fileMu.Lock()
	defer fileMu.Unlock()
	return h.list(provider, path)
}

func (h History) save(provider, path, operation string, current Snapshot) (Record, error) {
	var record Record
	if !filepath.IsAbs(h.Directory) || !filepath.IsAbs(path) || (provider != "codex" && provider != "claude") {
		return record, errors.New("备份目录和配置路径必须为绝对路径")
	}
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return record, errors.New("创建备份编号失败")
	}
	record = Record{ID: hex.EncodeToString(id[:]), CreatedAt: time.Now().UTC(), Provider: provider, Operation: operation, ConfigPath: path, Exists: current.Exists, Size: len(current.Data)}
	record.Name = defaultRecordName(record)
	record.Directory = h.recordDirectory(record)
	plain, err := json.Marshal(historySnapshot{Version: 1, Record: record, Snapshot: current, Revision: current.Revision()})
	if err != nil {
		return Record{}, errors.New("创建配置备份失败")
	}
	defer clear(plain)
	cipher, err := h.Vault.Seal(plain)
	if err != nil {
		return Record{}, errors.New("无法加密配置备份，未修改客户端配置")
	}
	if err := os.MkdirAll(h.Directory, 0700); err != nil {
		return Record{}, errors.New("创建备份目录失败，未修改客户端配置")
	}
	if err := os.Mkdir(record.Directory, 0700); err != nil {
		return Record{}, errors.New("创建备份记录目录失败，未修改客户端配置")
	}
	// Exclusive creation keeps every snapshot immutable, including after a crash.
	f, err := os.OpenFile(filepath.Join(record.Directory, "config.dpapi"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		_ = os.Remove(record.Directory)
		return Record{}, errors.New("创建备份文件失败，未修改客户端配置")
	}
	_, err = f.Write(cipher)
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil || closeErr != nil {
		_ = os.Remove(f.Name())
		_ = os.Remove(record.Directory)
		return Record{}, errors.New("保存配置备份失败，未修改客户端配置")
	}
	return record, nil
}

func (h History) Rename(provider, path, id, name string) error {
	fileMu.Lock()
	defer fileMu.Unlock()
	name, err := normalizeRecordName(name)
	if err != nil {
		return err
	}
	b, err := h.load(id)
	if err != nil {
		return err
	}
	defer clear(b.Snapshot.Data)
	if b.Record.Provider != provider || filepath.Clean(b.Record.ConfigPath) != filepath.Clean(path) {
		return errors.New("备份不属于当前客户端配置")
	}
	if err := h.organize(b.Record); err != nil {
		return err
	}
	metadata, err := json.Marshal(struct {
		Name string `json:"name"`
	}{Name: name})
	if err != nil {
		return err
	}
	return writeAtomic(filepath.Join(b.Record.Directory, "record.json"), metadata, 0600)
}

func (h History) replace(provider, path, revision, operation string, target Snapshot) (Record, error) {
	current, err := Read(path)
	if err != nil {
		return Record{}, err
	}
	defer clear(current.Data)
	if revision == "" || current.Revision() != revision {
		return Record{}, errors.New("配置已变化，请重新预览后再操作")
	}
	if current.Revision() == target.Revision() {
		// The first application starts management even if the existing client
		// configuration already matches. Later identical applications need no
		// extra snapshot and do not replace the live file.
		records, err := h.list(provider, path)
		if err != nil {
			return Record{}, err
		}
		if len(records) > 0 {
			return Record{}, nil
		}
	}
	record, err := h.save(provider, path, operation, current)
	if err != nil {
		return Record{}, err
	}
	latest, err := Read(path)
	if err != nil {
		return record, err
	}
	defer clear(latest.Data)
	if latest.Revision() != revision {
		return record, errors.New("配置已变化，未写入；请重新预览")
	}
	if current.Revision() == target.Revision() {
		return record, nil
	}
	if !target.Exists {
		if current.Exists {
			if err = os.Remove(path); err != nil {
				return record, errors.New("移除客户端配置失败，备份已保留")
			}
		}
		return record, nil
	}
	mode := target.Mode
	if mode == 0 {
		mode = current.Mode
	}
	if mode == 0 {
		mode = 0600
	}
	return record, writeAtomic(path, target.Data, mode)
}

func (h History) Apply(provider, path, revision, operation string, content []byte) (Record, error) {
	fileMu.Lock()
	defer fileMu.Unlock()
	if len(content) > maxSize {
		return Record{}, errors.New("配置文件过大")
	}
	return h.replace(provider, path, revision, operation, Snapshot{Data: content, Exists: true})
}

// An explicit selection plus the latest revision authorizes restoring even when
// the current file was edited outside Vane. That file is backed up first.
func (h History) Restore(provider, path, id, revision string) error {
	fileMu.Lock()
	defer fileMu.Unlock()
	b, err := h.load(id)
	if err != nil {
		return err
	}
	defer clear(b.Snapshot.Data)
	if b.Record.Provider != provider || filepath.Clean(b.Record.ConfigPath) != filepath.Clean(path) {
		return errors.New("备份不属于当前客户端配置，未恢复")
	}
	_, err = h.replace(provider, path, revision, "before_restore", b.Snapshot)
	return err
}
