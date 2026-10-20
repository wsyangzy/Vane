package app

import (
	"fmt"
	"os"
	"path/filepath"
)

type SaveFileResult struct {
	Success  bool   `json:"success"`
	FilePath string `json:"file_path"`
	FileName string `json:"file_name"`
	FileSize int64  `json:"file_size"` // 字节数
	Error    string `json:"error,omitempty"`
}

func (a *App) SaveLogFile(content string, defaultFileName string) SaveFileResult {
	if a.wailsApp == nil {
		return SaveFileResult{Success: false, Error: "应用未初始化"}
	}

	// 使用 Wails v3 原生保存文件对话框
	filePath, err := a.wailsApp.Dialog.SaveFile().
		SetMessage("保存日志文件").
		SetFilename(defaultFileName).
		AddFilter("文本文件", "*.txt").
		AddFilter("日志文件", "*.log").
		AddFilter("所有文件", "*.*").
		PromptForSingleSelection()

	if err != nil {
		return SaveFileResult{Success: false, Error: fmt.Sprintf("打开保存对话框失败: %v", err)}
	}

	// 用户取消了对话框
	if filePath == "" {
		return SaveFileResult{Success: false, Error: "cancelled"}
	}

	// 写入文件
	err = os.WriteFile(filePath, []byte(content), 0644)
	if err != nil {
		return SaveFileResult{Success: false, Error: fmt.Sprintf("写入文件失败: %v", err)}
	}

	// 获取文件信息
	fileInfo, _ := os.Stat(filePath)
	var fileSize int64
	if fileInfo != nil {
		fileSize = fileInfo.Size()
	}

	return SaveFileResult{
		Success:  true,
		FilePath: filePath,
		FileName: filepath.Base(filePath),
		FileSize: fileSize,
	}
}
