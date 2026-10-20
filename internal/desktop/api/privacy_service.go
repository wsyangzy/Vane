package api

import (
	"github.com/wsyangzy/Vane/internal/app"
	"github.com/wsyangzy/Vane/internal/privacy"
)

type PrivacyService struct{ app *app.App }

func (s *PrivacyService) GetPrivacyOverview() (privacy.Overview, error) {
	return s.app.GetPrivacyOverview()
}

func (s *PrivacyService) UpdatePrivacySettings(settings privacy.Settings) error {
	return s.app.UpdatePrivacySettings(settings)
}

func (s *PrivacyService) CreatePrivacyRule(rule privacy.Rule) (privacy.StoredRule, error) {
	return s.app.CreatePrivacyRule(rule)
}

func (s *PrivacyService) UpdatePrivacyRule(id int64, rule privacy.Rule) (privacy.StoredRule, error) {
	return s.app.UpdatePrivacyRule(id, rule)
}

func (s *PrivacyService) DeletePrivacyRule(id int64) error {
	return s.app.DeletePrivacyRule(id)
}

func (s *PrivacyService) DeletePrivacyRules(ids []int64) (int64, error) {
	return s.app.DeletePrivacyRules(ids)
}

func (s *PrivacyService) SetPrivacyRulesEnabled(ids []int64, enabled bool) (int64, error) {
	return s.app.SetPrivacyRulesEnabled(ids, enabled)
}

func (s *PrivacyService) ApplyPrivacyPreset(presetID string) (int, error) {
	return s.app.ApplyPrivacyPreset(presetID)
}

func (s *PrivacyService) DiscardLegacyPrivacyRules() error {
	return s.app.DiscardLegacyPrivacyRules()
}

func (s *PrivacyService) PreviewPrivacyRedaction(input string) (privacy.PreviewResult, error) {
	return s.app.PreviewPrivacyRedaction(input)
}

func (s *PrivacyService) PreviewPrivacyRequest(path, body string) (privacy.RequestPreviewResult, error) {
	return s.app.PreviewPrivacyRequest(path, body)
}

func (s *PrivacyService) GetPrivacyUnscannedPaths() ([]privacy.UnscannedPathStat, error) {
	return s.app.GetPrivacyUnscannedPaths()
}

func (s *PrivacyService) ClearPrivacyUnscannedPaths() error {
	return s.app.ClearPrivacyUnscannedPaths()
}
