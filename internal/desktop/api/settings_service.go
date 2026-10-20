package api

import (
	"github.com/wsyangzy/Vane/internal/app"
)

type SettingsService struct{ app *app.App }

func (s *SettingsService) GetSettingsStorageStatus() app.SettingsStorageStatus {
	return s.app.GetSettingsStorageStatus()
}

func (s *SettingsService) GetSettingCategories() []app.CategoryInfo {
	return s.app.GetSettingCategories()
}

func (s *SettingsService) GetAllSettings() ([]app.SettingInfo, error) {
	return s.app.GetAllSettings()
}

func (s *SettingsService) GetSettingsByCategory(category string) ([]app.SettingInfo, error) {
	return s.app.GetSettingsByCategory(category)
}

func (s *SettingsService) GetSettingsByScope(input app.ScopedSettingsInput) ([]app.SettingInfo, error) {
	return s.app.GetSettingsByScope(input)
}

func (s *SettingsService) GetSetting(category, key string) (app.SettingInfo, error) {
	return s.app.GetSetting(category, key)
}

func (s *SettingsService) UpdateSetting(input app.UpdateSettingInput) error {
	return s.app.UpdateSetting(input)
}

func (s *SettingsService) BatchUpdateSettings(input app.BatchUpdateSettingsInput) error {
	return s.app.BatchUpdateSettings(input)
}

func (s *SettingsService) ResetSettingsByScope(input app.ScopedSettingsInput) error {
	return s.app.ResetSettingsByScope(input)
}

func (s *SettingsService) ResetCategorySettings(category string) error {
	return s.app.ResetCategorySettings(category)
}

func (s *SettingsService) GetPortInfo() app.PortInfo {
	return s.app.GetPortInfo()
}

func (s *SettingsService) UpdatePreferredPort(port int) error {
	return s.app.UpdatePreferredPort(port)
}

func (s *SettingsService) CheckPortAvailable(port int) bool {
	return s.app.CheckPortAvailable(port)
}

func (s *SettingsService) GetConfigSourceInfo() ([]app.ConfigSourceInfo, error) {
	return s.app.GetConfigSourceInfo()
}
