package api

import (
	"github.com/wsyangzy/Vane/internal/app"
)

type SystemService struct{ app *app.App }

func (s *SystemService) GetAppState() map[string]string {
	return s.app.GetAppState()
}

func (s *SystemService) ShowMainWindow() {
	s.app.ShowMainWindow()
}

func (s *SystemService) HideMainWindow() {
	s.app.HideMainWindow()
}

func (s *SystemService) RequestQuit() {
	s.app.RequestQuit()
}

func (s *SystemService) OpenLogDir() error {
	return s.app.OpenLogDir()
}

func (s *SystemService) OpenDataDir() error {
	return s.app.OpenDataDir()
}

func (s *SystemService) OpenClaudeConfigDir() error {
	return s.app.OpenClaudeConfigDir()
}

func (s *SystemService) OpenCodexConfigDir() error {
	return s.app.OpenCodexConfigDir()
}

func (s *SystemService) GetProxyAddress() string {
	return s.app.GetProxyAddress()
}

func (s *SystemService) GetSystemStatus() app.SystemStatus {
	return s.app.GetSystemStatus()
}

func (s *SystemService) GetClaudeCodeVersion() string {
	return s.app.GetClaudeCodeVersion()
}

func (s *SystemService) GetCodexVersion() string {
	return s.app.GetCodexVersion()
}

func (s *SystemService) GetPortConflictStatus() app.PortConflictStatus {
	return s.app.GetPortConflictStatus()
}

func (s *SystemService) ResolvePortConflict(action string) app.PortConflictResult {
	return s.app.ResolvePortConflict(action)
}

func (s *SystemService) GetConfig() app.ConfigInfo {
	return s.app.GetConfig()
}

func (s *SystemService) GetProxyURL() string {
	return s.app.GetProxyURL()
}

func (s *SystemService) IsProxyRunning() bool {
	return s.app.IsProxyRunning()
}

func (s *SystemService) GetSuspendedRequestsCount() int {
	return s.app.GetSuspendedRequestsCount()
}

func (s *SystemService) GetSuspendedEndpointCounts() map[string]int {
	return s.app.GetSuspendedEndpointCounts()
}

func (s *SystemService) GetManualRetryRequestsCount() int {
	return s.app.GetManualRetryRequestsCount()
}

func (s *SystemService) GetManualRetryChannelCounts() map[string]int {
	return s.app.GetManualRetryChannelCounts()
}

func (s *SystemService) GetSystemMetrics() (*app.SystemMetrics, error) {
	return s.app.GetSystemMetrics()
}
