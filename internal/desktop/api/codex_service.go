package api

import (
	"github.com/wsyangzy/Vane/internal/app"
)

type CodexService struct{ app *app.App }

func (s *CodexService) GetCodexProviderStorageStatus() app.CodexProviderStorageStatus {
	return s.app.GetCodexProviderStorageStatus()
}

func (s *CodexService) GetCodexChannels() ([]app.CodexChannelInfo, error) {
	return s.app.GetCodexChannels()
}

func (s *CodexService) GetNextAvailableCodexChannel(excludeID int64) (app.CodexChannelCandidateInfo, error) {
	return s.app.GetNextAvailableCodexChannel(excludeID)
}

func (s *CodexService) CreateCodexChannel(input app.CreateCodexChannelInput) error {
	return s.app.CreateCodexChannel(input)
}

func (s *CodexService) UpdateCodexChannelByID(id int64, input app.UpdateCodexChannelInput) error {
	return s.app.UpdateCodexChannelByID(id, input)
}

func (s *CodexService) SetCodexChannelFailoverEnabled(id int64, enabled bool) error {
	return s.app.SetCodexChannelFailoverEnabled(id, enabled)
}

func (s *CodexService) DeleteCodexChannelByID(id int64) error {
	return s.app.DeleteCodexChannelByID(id)
}

func (s *CodexService) ActivateCodexChannel(id int64) error {
	return s.app.ActivateCodexChannel(id)
}

func (s *CodexService) CreateCodexCredentialGroup(input app.CreateCodexCredentialGroupInput) (app.CodexCredentialGroupInfo, error) {
	return s.app.CreateCodexCredentialGroup(input)
}

func (s *CodexService) UpdateCodexCredentialGroupByID(id int64, input app.UpdateCodexCredentialGroupInput) (app.CodexCredentialGroupInfo, error) {
	return s.app.UpdateCodexCredentialGroupByID(id, input)
}

func (s *CodexService) DeleteCodexCredentialGroupByID(id int64) error {
	return s.app.DeleteCodexCredentialGroupByID(id)
}

func (s *CodexService) SetDefaultCodexCredentialGroup(id int64) error {
	return s.app.SetDefaultCodexCredentialGroup(id)
}

func (s *CodexService) CreateCodexEndpoint(input app.CreateCodexEndpointInput) error {
	return s.app.CreateCodexEndpoint(input)
}

func (s *CodexService) UpdateCodexEndpointByID(id int64, input app.UpdateCodexEndpointInput) error {
	return s.app.UpdateCodexEndpointByID(id, input)
}

func (s *CodexService) DeleteCodexEndpointByID(id int64) error {
	return s.app.DeleteCodexEndpointByID(id)
}

func (s *CodexService) ActivateCodexEndpoint(id int64) error {
	return s.app.ActivateCodexEndpoint(id)
}

func (s *CodexService) GetCodexProviders() ([]app.CodexProviderInfo, error) {
	return s.app.GetCodexProviders()
}

func (s *CodexService) CreateCodexProvider(input app.CreateCodexProviderInput) error {
	return s.app.CreateCodexProvider(input)
}

func (s *CodexService) UpdateCodexProviderByID(id int64, input app.UpdateCodexProviderInput) error {
	return s.app.UpdateCodexProviderByID(id, input)
}

func (s *CodexService) DeleteCodexProviderByID(id int64) error {
	return s.app.DeleteCodexProviderByID(id)
}

func (s *CodexService) ActivateCodexProvider(id int64) error {
	return s.app.ActivateCodexProvider(id)
}

func (s *CodexService) GetCodexSuspendedCount() int {
	return s.app.GetCodexSuspendedCount()
}

func (s *CodexService) GetCodexGlobalConfig() (app.CodexGlobalConfigInfo, error) {
	return s.app.GetCodexGlobalConfig()
}

func (s *CodexService) UpdateCodexGlobalConfig(input app.CodexGlobalConfigInput) error {
	return s.app.UpdateCodexGlobalConfig(input)
}
