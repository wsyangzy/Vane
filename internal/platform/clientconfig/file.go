// Package clientconfig provides optimistic file updates and encrypted rollback.
// Client-specific fields and authentication belong to provider services.
package clientconfig

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
)

const maxSize = 4 << 20

type Vault interface {
	Seal([]byte) ([]byte, error)
	Open([]byte) ([]byte, error)
}

type Snapshot struct {
	Data   []byte
	Exists bool
	Mode   os.FileMode
}

func (s Snapshot) Revision() string {
	h := sha256.New()
	if s.Exists {
		h.Write([]byte{1})
	}
	h.Write(s.Data)
	return hex.EncodeToString(h.Sum(nil))
}

type backup struct {
	Path     string
	Original Snapshot
	Applied  string
	Previous string // Allows recovery if replacement failed after saving the backup.
}

// A single lock also serializes updates from separate App instances in tests.
var fileMu sync.Mutex

func Read(path string) (Snapshot, error) {
	return read(path, maxSize)
}

func read(path string, limit int64) (Snapshot, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return Snapshot{}, nil
	}
	if err != nil || !info.Mode().IsRegular() || info.Size() > limit {
		return Snapshot{}, errors.New("配置文件不可读、过大或不是普通文件")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Snapshot{}, errors.New("读取配置文件失败")
	}
	return Snapshot{Data: data, Exists: true, Mode: info.Mode().Perm()}, nil
}

func backupPath(path string) string { return path + ".relay-subscription.dpapi" }

func BackupAvailable(path string) bool {
	info, err := os.Lstat(backupPath(path))
	return err == nil && info.Mode().IsRegular() && info.Size() <= maxSize*2
}

func loadBackup(path string, vault Vault) (*backup, error) {
	s, err := read(backupPath(path), maxSize*2)
	if err != nil {
		return nil, err
	}
	if !s.Exists {
		return nil, nil
	}
	plain, err := vault.Open(s.Data)
	if err != nil {
		return nil, errors.New("无法解密配置备份，请使用创建备份的 Windows 用户")
	}
	defer clear(plain)
	var b backup
	if json.Unmarshal(plain, &b) != nil || b.Applied == "" || b.Path != path {
		return nil, errors.New("配置备份无效")
	}
	return &b, nil
}

func Apply(path, revision string, content []byte, vault Vault) error {
	fileMu.Lock()
	defer fileMu.Unlock()
	current, err := Read(path)
	if err != nil {
		return err
	}
	if revision == "" || current.Revision() != revision {
		return errors.New("配置已变化，请重新预览后再应用")
	}
	b, err := loadBackup(path, vault)
	if err != nil {
		return err
	}
	if b == nil {
		b = &backup{Path: path, Original: current}
	} else if current.Revision() != b.Applied && current.Revision() != b.Previous && current.Revision() != b.Original.Revision() {
		return errors.New("配置在接入后被外部修改，请先手动合并或恢复；不会覆盖原备份")
	}
	b.Previous = current.Revision()
	b.Applied = (Snapshot{Data: content, Exists: true}).Revision()
	plain, err := json.Marshal(b)
	if err != nil {
		return errors.New("创建配置备份失败")
	}
	defer clear(plain)
	cipher, err := vault.Seal(plain)
	if err != nil {
		return errors.New("无法加密配置备份，未修改客户端配置")
	}
	if err = writeAtomic(backupPath(path), cipher, 0600); err != nil {
		return err
	}
	// Check again after preparing the backup, before replacing the client file.
	latest, err := Read(path)
	if err != nil {
		return err
	}
	if latest.Revision() != revision {
		return errors.New("配置已变化，未应用；请重新预览")
	}
	mode := current.Mode
	if mode == 0 {
		mode = 0600
	}
	return writeAtomic(path, content, mode)
}

func Restore(path, revision string, vault Vault) error {
	fileMu.Lock()
	defer fileMu.Unlock()
	current, err := Read(path)
	if err != nil {
		return err
	}
	b, err := loadBackup(path, vault)
	if err != nil {
		return err
	}
	if b == nil {
		return errors.New("没有可恢复的订阅接入备份")
	}
	if revision == "" || current.Revision() != revision || (current.Revision() != b.Applied && current.Revision() != b.Previous && current.Revision() != b.Original.Revision()) {
		return errors.New("配置已被修改，无法安全恢复；请先手动合并，备份已保留")
	}
	if b.Original.Exists {
		if err = writeAtomic(path, b.Original.Data, b.Original.Mode); err != nil {
			return err
		}
	} else if current.Exists {
		if err = os.Remove(path); err != nil {
			return errors.New("移除接入配置失败，备份已保留")
		}
	}
	if err = os.Remove(backupPath(path)); err != nil {
		return errors.New("配置已恢复，但备份清理失败")
	}
	return nil
}

func writeAtomic(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return errors.New("创建配置目录失败")
	}
	if _, err := read(path, maxSize*2); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".relay-config-*")
	if err != nil {
		return errors.New("创建配置临时文件失败")
	}
	temp := f.Name()
	defer os.Remove(temp)
	if err = f.Chmod(mode); err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil || closeErr != nil {
		return errors.New("写入配置失败，原配置未替换")
	}
	if err = os.Rename(temp, path); err != nil {
		return errors.New("替换配置失败，请关闭占用配置文件的程序后重试")
	}
	return nil
}
