package api

import (
	"github.com/wsyangzy/Vane/internal/app"
)

type ClaudeService struct{ app *app.App }

func (s *ClaudeService) GetEndpoints() []app.EndpointInfo {
	return s.app.GetEndpoints()
}

func (s *ClaudeService) SetEndpointPriority(name string, priority int) error {
	return s.app.SetEndpointPriority(name, priority)
}

func (s *ClaudeService) TriggerHealthCheck(name string) error {
	return s.app.TriggerHealthCheck(name)
}

func (s *ClaudeService) BatchHealthCheckAll() app.BatchHealthCheckResult {
	return s.app.BatchHealthCheckAll()
}

func (s *ClaudeService) GetKeysOverview() app.KeysOverviewResult {
	return s.app.GetKeysOverview()
}

func (s *ClaudeService) SwitchKey(endpointName, keyType string, index int) (app.SwitchKeyResult, error) {
	return s.app.SwitchKey(endpointName, keyType, index)
}

func (s *ClaudeService) GetGroups() []app.GroupInfo {
	return s.app.GetGroups()
}

func (s *ClaudeService) GetNextAvailableChannel(excludeChannel string) (string, error) {
	return s.app.GetNextAvailableChannel(excludeChannel)
}

func (s *ClaudeService) ActivateGroup(name string) error {
	return s.app.ActivateGroup(name)
}

func (s *ClaudeService) PauseGroup(name string) error {
	return s.app.PauseGroup(name)
}

func (s *ClaudeService) IsInterChannelFailoverEnabled() bool {
	return s.app.IsInterChannelFailoverEnabled()
}

func (s *ClaudeService) ResumeGroup(name string) error {
	return s.app.ResumeGroup(name)
}

func (s *ClaudeService) GetEndpointStorageStatus() app.EndpointStorageStatus {
	return s.app.GetEndpointStorageStatus()
}

func (s *ClaudeService) GetEndpointRecords() ([]app.EndpointRecordInfo, error) {
	return s.app.GetEndpointRecords()
}

func (s *ClaudeService) GetEndpointRecord(name string) (app.EndpointRecordInfo, error) {
	return s.app.GetEndpointRecord(name)
}

func (s *ClaudeService) CreateEndpointRecord(input app.CreateEndpointInput) error {
	return s.app.CreateEndpointRecord(input)
}

func (s *ClaudeService) UpdateEndpointRecord(name string, input app.CreateEndpointInput) error {
	return s.app.UpdateEndpointRecord(name, input)
}

func (s *ClaudeService) UpdateEndpointRecordByID(id int64, input app.CreateEndpointInput) error {
	return s.app.UpdateEndpointRecordByID(id, input)
}

func (s *ClaudeService) SetEndpointFailoverEnabled(name string, enabled bool) error {
	return s.app.SetEndpointFailoverEnabled(name, enabled)
}

func (s *ClaudeService) SetEndpointFailoverEnabledByID(id int64, enabled bool) error {
	return s.app.SetEndpointFailoverEnabledByID(id, enabled)
}

func (s *ClaudeService) SetEndpointSkipHealthCheckByID(id int64, skip bool) error {
	return s.app.SetEndpointSkipHealthCheckByID(id, skip)
}

func (s *ClaudeService) DeleteEndpointRecord(name string) error {
	return s.app.DeleteEndpointRecord(name)
}

func (s *ClaudeService) DeleteEndpointRecordByID(id int64) error {
	return s.app.DeleteEndpointRecordByID(id)
}

func (s *ClaudeService) ToggleEndpointRecord(name string, enabled bool) error {
	return s.app.ToggleEndpointRecord(name, enabled)
}

func (s *ClaudeService) ToggleEndpointRecordByID(id int64, enabled bool) error {
	return s.app.ToggleEndpointRecordByID(id, enabled)
}

func (s *ClaudeService) ActivateEndpointRecordByID(id int64) error {
	return s.app.ActivateEndpointRecordByID(id)
}

func (s *ClaudeService) GetChannels() ([]app.ChannelInfo, error) {
	return s.app.GetChannels()
}

func (s *ClaudeService) CreateClaudeCredentialGroup(input app.CreateClaudeCredentialGroupInput) (app.ClaudeCredentialGroupInfo, error) {
	return s.app.CreateClaudeCredentialGroup(input)
}

func (s *ClaudeService) UpdateClaudeCredentialGroupByID(id int64, input app.CreateClaudeCredentialGroupInput) (app.ClaudeCredentialGroupInfo, error) {
	return s.app.UpdateClaudeCredentialGroupByID(id, input)
}

func (s *ClaudeService) DeleteClaudeCredentialGroupByID(id int64) error {
	return s.app.DeleteClaudeCredentialGroupByID(id)
}

func (s *ClaudeService) SetDefaultClaudeCredentialGroup(id int64) error {
	return s.app.SetDefaultClaudeCredentialGroup(id)
}

func (s *ClaudeService) CreateChannel(input app.CreateChannelInput) error {
	return s.app.CreateChannel(input)
}

func (s *ClaudeService) UpdateChannel(input app.UpdateChannelInput) error {
	return s.app.UpdateChannel(input)
}

func (s *ClaudeService) DeleteChannel(name string, deleteEndpoints bool) error {
	return s.app.DeleteChannel(name, deleteEndpoints)
}

func (s *ClaudeService) GetEndpointsByChannel(channel string) ([]app.EndpointRecordInfo, error) {
	return s.app.GetEndpointsByChannel(channel)
}
