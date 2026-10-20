package api

import (
	"github.com/wsyangzy/Vane/internal/app"
	"github.com/wsyangzy/Vane/internal/platform/logging"
)

type LogsService struct{ app *app.App }

func (s *LogsService) SaveLogFile(content string, defaultFileName string) app.SaveFileResult {
	return s.app.SaveLogFile(content, defaultFileName)
}

func (s *LogsService) GetRecentLogs(limit int) []logging.LogEntry {
	return s.app.GetRecentLogs(limit)
}

func (s *LogsService) StartLogStream() {
	s.app.StartLogStream()
}

func (s *LogsService) StopLogStream() {
	s.app.StopLogStream()
}

func (s *LogsService) GetLogStreamStatus() bool {
	return s.app.GetLogStreamStatus()
}
