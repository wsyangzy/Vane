package clientconfig

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func WriteCodexLiveFiles(
	authPath string,
	originalAuth []byte,
	authExisted bool,
	updatedAuth []byte,
	configPath string,
	originalConfig []byte,
	configExisted bool,
	updatedConfig []byte,
) error {
	if err := WriteCodexFile(authPath, updatedAuth); err != nil {
		if rollbackErr := RestoreCodexFile(authPath, originalAuth, authExisted); rollbackErr != nil {
			return fmt.Errorf("写入 Codex auth.json 失败 (%s): %w；回滚也失败: %v", authPath, err, rollbackErr)
		}
		return fmt.Errorf("写入 Codex auth.json 失败 (%s): %w", authPath, err)
	}

	if err := WriteCodexFile(configPath, updatedConfig); err != nil {
		configRollbackErr := RestoreCodexFile(configPath, originalConfig, configExisted)
		authRollbackErr := RestoreCodexFile(authPath, originalAuth, authExisted)

		if configRollbackErr != nil || authRollbackErr != nil {
			return fmt.Errorf(
				"写入 Codex 配置文件失败 (%s): %w；config 回滚失败: %v；auth 回滚失败: %v",
				configPath,
				err,
				configRollbackErr,
				authRollbackErr,
			)
		}

		return fmt.Errorf("写入 Codex 配置文件失败 (%s): %w", configPath, err)
	}

	return nil
}

func WriteCodexFile(path string, content []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, content, 0644)
}

func RestoreCodexFile(path string, original []byte, existed bool) error {
	if existed {
		return WriteCodexFile(path, original)
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func GetCodexHomeDir() (string, error) {
	if codexHome := strings.TrimSpace(os.Getenv("CODEX_HOME")); codexHome != "" {
		return codexHome, nil
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("获取用户主目录失败: %w", err)
	}

	return filepath.Join(homeDir, ".codex"), nil
}

func GetCodexConfigPath() (string, error) {
	codexHome, err := GetCodexHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(codexHome, "config.toml"), nil
}

func GetCodexAuthPath() (string, error) {
	codexHome, err := GetCodexHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(codexHome, "auth.json"), nil
}
