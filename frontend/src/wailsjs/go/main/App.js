// Wails v2 → v3 App Bindings Compatibility Shim
// Uses @wailsio/runtime Call.ByName() instead of window.go.*
// Generated from 74 Go methods via wails3 generate bindings

import { Call } from '@wailsio/runtime';

export function ActivateGroup(arg1) {
  return Call.ByName("main.App.ActivateGroup", arg1);
}

export function ActivateCodexProvider(arg1) {
  return Call.ByName("main.App.ActivateCodexProvider", arg1);
}

export function BatchHealthCheckAll() {
  return Call.ByName("main.App.BatchHealthCheckAll");
}

export function BatchUpdateSettings(arg1) {
  return Call.ByName("main.App.BatchUpdateSettings", arg1);
}

export function ClearCache(arg1) {
  return Call.ByName("main.App.ClearCache", arg1);
}

export function CheckPortAvailable(arg1) {
  return Call.ByName("main.App.CheckPortAvailable", arg1);
}

export function CreateChannel(arg1) {
  return Call.ByName("main.App.CreateChannel", arg1);
}

export function CreateClaudeCredentialGroup(arg1) {
  return Call.ByName("main.App.CreateClaudeCredentialGroup", arg1);
}

export function CreateCodexProvider(arg1) {
  return Call.ByName("main.App.CreateCodexProvider", arg1);
}

export function CreateEndpointRecord(arg1) {
  return Call.ByName("main.App.CreateEndpointRecord", arg1);
}

export function CreateModelPricing(arg1) {
  return Call.ByName("main.App.CreateModelPricing", arg1);
}

export function DeletePrivacyRules(arg1) {
  return Call.ByName("main.App.DeletePrivacyRules", arg1);
}

export function SetPrivacyRulesEnabled(arg1, arg2) {
  return Call.ByName("main.App.SetPrivacyRulesEnabled", arg1, arg2);
}

export function GetPrivacyOverview() {
  return Call.ByName("main.App.GetPrivacyOverview");
}

export function UpdatePrivacySettings(arg1) {
  return Call.ByName("main.App.UpdatePrivacySettings", arg1);
}

export function ApplyPrivacyPreset(arg1) {
  return Call.ByName("main.App.ApplyPrivacyPreset", arg1);
}

export function DiscardLegacyPrivacyRules() {
  return Call.ByName("main.App.DiscardLegacyPrivacyRules");
}

export function GetPrivacyUnscannedPaths() {
  return Call.ByName("main.App.GetPrivacyUnscannedPaths");
}

export function ClearPrivacyUnscannedPaths() {
  return Call.ByName("main.App.ClearPrivacyUnscannedPaths");
}

export function CreatePrivacyRule(arg1) {
  return Call.ByName("main.App.CreatePrivacyRule", arg1);
}


export function DeleteChannel(arg1, arg2) {
  return Call.ByName("main.App.DeleteChannel", arg1, arg2);
}

export function DeleteClaudeCredentialGroupByID(arg1) {
  return Call.ByName("main.App.DeleteClaudeCredentialGroupByID", arg1);
}

export function DeleteCodexProviderByID(arg1) {
  return Call.ByName("main.App.DeleteCodexProviderByID", arg1);
}

export function DeleteEndpointRecord(arg1) {
  return Call.ByName("main.App.DeleteEndpointRecord", arg1);
}

export function DeleteEndpointRecordByID(arg1) {
  return Call.ByName("main.App.DeleteEndpointRecordByID", arg1);
}

export function DeleteModelPricing(arg1, arg2) {
  return Call.ByName("main.App.DeleteModelPricing", arg1, arg2);
}

export function DeleteModelPricingTier(arg1, arg2, arg3) {
  return Call.ByName("main.App.DeleteModelPricingTier", arg1, arg2, arg3);
}

export function DeletePrivacyRule(arg1) {
  return Call.ByName("main.App.DeletePrivacyRule", arg1);
}


export function GetAllSettings() {
  return Call.ByName("main.App.GetAllSettings");
}

export function GetCacheStatus() {
  return Call.ByName("main.App.GetCacheStatus");
}

export function GetChannels() {
  return Call.ByName("main.App.GetChannels");
}

export function GetClaudeCodeVersion() {
  return Call.ByName("main.App.GetClaudeCodeVersion");
}

export function GetCodexVersion() {
  return Call.ByName("main.App.GetCodexVersion");
}

export function GetConfig() {
  return Call.ByName("main.App.GetConfig");
}

export function GetCodexProviders() {
  return Call.ByName("main.App.GetCodexProviders");
}

export function GetCodexProviderStorageStatus() {
  return Call.ByName("main.App.GetCodexProviderStorageStatus");
}

export function GetConnectionActivityChart(arg1, arg2) {
  return Call.ByName("main.App.GetConnectionActivityChart", arg1, arg2);
}

export function GetEndpointCosts(arg1) {
  return Call.ByName("main.App.GetEndpointCosts", arg1);
}

export function GetEndpointAvailability(arg1, arg2, arg3, arg4) {
  return Call.ByName("main.App.GetEndpointAvailability", arg1, arg2, arg3, arg4);
}

export function GetEndpointHealthChart() {
  return Call.ByName("main.App.GetEndpointHealthChart");
}

export function GetCodexEndpointHealthChart() {
  return Call.ByName("main.App.GetCodexEndpointHealthChart");
}

export function GetEndpointRecord(arg1) {
  return Call.ByName("main.App.GetEndpointRecord", arg1);
}

export function GetEndpointRecords() {
  return Call.ByName("main.App.GetEndpointRecords");
}

export function GetEndpointStorageStatus() {
  return Call.ByName("main.App.GetEndpointStorageStatus");
}

export function GetEndpoints() {
  return Call.ByName("main.App.GetEndpoints");
}

export function GetEndpointsByChannel(arg1) {
  return Call.ByName("main.App.GetEndpointsByChannel", arg1);
}

export function GetGroups() {
  return Call.ByName("main.App.GetGroups");
}

export function GetKeysOverview() {
  return Call.ByName("main.App.GetKeysOverview");
}

export function GetLogStreamStatus() {
  return Call.ByName("main.App.GetLogStreamStatus");
}

export function GetManualRetryChannelCounts() {
  return Call.ByName("main.App.GetManualRetryChannelCounts");
}

export function GetManualRetryRequestsCount() {
  return Call.ByName("main.App.GetManualRetryRequestsCount");
}

export function GetModelPricing(arg1) {
  return Call.ByName("main.App.GetModelPricing", arg1);
}

export function GetModelPricingStorageStatus() {
  return Call.ByName("main.App.GetModelPricingStorageStatus");
}

export function GetModelPricings() {
  return Call.ByName("main.App.GetModelPricings");
}

export function GetModelPricingsByProvider(arg1) {
  return Call.ByName("main.App.GetModelPricingsByProvider", arg1);
}

export function GetNextAvailableChannel(arg1) {
  return Call.ByName("main.App.GetNextAvailableChannel", arg1);
}

export function GetPortConflictStatus() {
  return Call.ByName("main.App.GetPortConflictStatus");
}

export function GetPortInfo() {
  return Call.ByName("main.App.GetPortInfo");
}

export function GetProxyURL() {
  return Call.ByName("main.App.GetProxyURL");
}



export function GetRecentLogs(arg1) {
  return Call.ByName("main.App.GetRecentLogs", arg1);
}

export function GetRequestTrendChart(arg1, arg2) {
  return Call.ByName("main.App.GetRequestTrendChart", arg1, arg2);
}

export function GetRequests(arg1) {
  return Call.ByName("main.App.GetRequests", arg1);
}

export function GetResponseTimeChart(arg1, arg2) {
  return Call.ByName("main.App.GetResponseTimeChart", arg1, arg2);
}

export function GetSetting(arg1, arg2) {
  return Call.ByName("main.App.GetSetting", arg1, arg2);
}

export function GetSettingCategories() {
  return Call.ByName("main.App.GetSettingCategories");
}

export function GetSettingsByCategory(arg1) {
  return Call.ByName("main.App.GetSettingsByCategory", arg1);
}

export function GetSettingsByScope(arg1) {
  return Call.ByName("main.App.GetSettingsByScope", arg1);
}

export function GetSettingsStorageStatus() {
  return Call.ByName("main.App.GetSettingsStorageStatus");
}

export function GetCredentialGroupDailyUsage(arg1, arg2, arg3, arg4) {
  return Call.ByName("main.App.GetCredentialGroupDailyUsage", arg1, arg2, arg3, arg4);
}

export function GetSuspendedEndpointCounts() {
  return Call.ByName("main.App.GetSuspendedEndpointCounts");
}

export function GetSuspendedRequestsCount() {
  return Call.ByName("main.App.GetSuspendedRequestsCount");
}

export function GetSystemStatus() {
  return Call.ByName("main.App.GetSystemStatus");
}

export function GetTokenUsage(arg1) {
  return Call.ByName("main.App.GetTokenUsage", arg1);
}

export function GetTokenUsageHeatmap(arg1) {
  return Call.ByName("main.App.GetTokenUsageHeatmap", arg1);
}

export function GetUsageStats(arg1) {
  return Call.ByName("main.App.GetUsageStats", arg1);
}

export function GetUsageSummary(arg1, arg2, arg3) {
  return Call.ByName("main.App.GetUsageSummary", arg1, arg2, arg3);
}

export function HideMainWindow() {
  return Call.ByName("main.App.HideMainWindow");
}

export function IsInterChannelFailoverEnabled() {
  return Call.ByName("main.App.IsInterChannelFailoverEnabled");
}

export function IsProxyRunning() {
  return Call.ByName("main.App.IsProxyRunning");
}

export function OpenClaudeConfigDir() {
  return Call.ByName("main.App.OpenClaudeConfigDir");
}

export function OpenCodexConfigDir() {
  return Call.ByName("main.App.OpenCodexConfigDir");
}

export function OpenDataDir() {
  return Call.ByName("main.App.OpenDataDir");
}

export function OpenLogDir() {
  return Call.ByName("main.App.OpenLogDir");
}

export function PreviewPrivacyRedaction(arg1) {
  return Call.ByName("main.App.PreviewPrivacyRedaction", arg1);
}

export function PauseGroup(arg1) {
  return Call.ByName("main.App.PauseGroup", arg1);
}

export function RequestQuit() {
  return Call.ByName("main.App.RequestQuit");
}

export function ResetCategorySettings(arg1) {
  return Call.ByName("main.App.ResetCategorySettings", arg1);
}

export function ResetSettingsByScope(arg1) {
  return Call.ByName("main.App.ResetSettingsByScope", arg1);
}

export function ResolvePortConflict(arg1) {
  return Call.ByName("main.App.ResolvePortConflict", arg1);
}

export function ResumeGroup(arg1) {
  return Call.ByName("main.App.ResumeGroup", arg1);
}

export function SaveLogFile(arg1, arg2) {
  return Call.ByName("main.App.SaveLogFile", arg1, arg2);
}

export function SetDefaultModelPricing(arg1) {
  return Call.ByName("main.App.SetDefaultModelPricing", arg1);
}

export function SetDefaultClaudeCredentialGroup(arg1) {
  return Call.ByName("main.App.SetDefaultClaudeCredentialGroup", arg1);
}


export function SetEndpointFailoverEnabled(arg1, arg2) {
  return Call.ByName("main.App.SetEndpointFailoverEnabled", arg1, arg2);
}

export function SetEndpointFailoverEnabledByID(arg1, arg2) {
  return Call.ByName("main.App.SetEndpointFailoverEnabledByID", arg1, arg2);
}

export function SetEndpointPriority(arg1, arg2) {
  return Call.ByName("main.App.SetEndpointPriority", arg1, arg2);
}

export function SetEndpointSkipHealthCheckByID(arg1, arg2) {
  return Call.ByName("main.App.SetEndpointSkipHealthCheckByID", arg1, arg2);
}

export function ShowMainWindow() {
  return Call.ByName("main.App.ShowMainWindow");
}

export function StartLogStream() {
  return Call.ByName("main.App.StartLogStream");
}

export function StopLogStream() {
  return Call.ByName("main.App.StopLogStream");
}

export function SwitchKey(arg1, arg2, arg3) {
  return Call.ByName("main.App.SwitchKey", arg1, arg2, arg3);
}

export function ToggleEndpointRecord(arg1, arg2) {
  return Call.ByName("main.App.ToggleEndpointRecord", arg1, arg2);
}

export function ToggleEndpointRecordByID(arg1, arg2) {
  return Call.ByName("main.App.ToggleEndpointRecordByID", arg1, arg2);
}

export function ActivateEndpointRecordByID(arg1) {
  return Call.ByName("main.App.ActivateEndpointRecordByID", arg1);
}

export function TriggerHealthCheck(arg1) {
  return Call.ByName("main.App.TriggerHealthCheck", arg1);
}

export function UpdateChannel(arg1) {
  return Call.ByName("main.App.UpdateChannel", arg1);
}

export function UpdateClaudeCredentialGroupByID(arg1, arg2) {
  return Call.ByName("main.App.UpdateClaudeCredentialGroupByID", arg1, arg2);
}

export function UpdateCodexProviderByID(arg1, arg2) {
  return Call.ByName("main.App.UpdateCodexProviderByID", arg1, arg2);
}

export function UpdateEndpointRecord(arg1, arg2) {
  return Call.ByName("main.App.UpdateEndpointRecord", arg1, arg2);
}

export function UpdateEndpointRecordByID(arg1, arg2) {
  return Call.ByName("main.App.UpdateEndpointRecordByID", arg1, arg2);
}

export function UpdateModelPricing(arg1, arg2) {
  return Call.ByName("main.App.UpdateModelPricing", arg1, arg2);
}

export function UpdatePrivacyRule(arg1, arg2) {
  return Call.ByName("main.App.UpdatePrivacyRule", arg1, arg2);
}

export function UpdatePreferredPort(arg1) {
  return Call.ByName("main.App.UpdatePreferredPort", arg1);
}

export function UpdateSetting(arg1) {
  return Call.ByName("main.App.UpdateSetting", arg1);
}

// ============ Codex Channel-Endpoint API（2026-05-13 新增） ============

export function GetCodexChannels() {
  return Call.ByName("main.App.GetCodexChannels");
}

export function CreateCodexChannel(arg1) {
  return Call.ByName("main.App.CreateCodexChannel", arg1);
}

export function UpdateCodexChannelByID(arg1, arg2) {
  return Call.ByName("main.App.UpdateCodexChannelByID", arg1, arg2);
}

export function SetCodexChannelFailoverEnabled(arg1, arg2) {
  return Call.ByName("main.App.SetCodexChannelFailoverEnabled", arg1, arg2);
}

export function DeleteCodexChannelByID(arg1) {
  return Call.ByName("main.App.DeleteCodexChannelByID", arg1);
}

export function ActivateCodexChannel(arg1) {
  return Call.ByName("main.App.ActivateCodexChannel", arg1);
}

export function CreateCodexCredentialGroup(arg1) {
  return Call.ByName("main.App.CreateCodexCredentialGroup", arg1);
}

export function UpdateCodexCredentialGroupByID(arg1, arg2) {
  return Call.ByName("main.App.UpdateCodexCredentialGroupByID", arg1, arg2);
}

export function DeleteCodexCredentialGroupByID(arg1) {
  return Call.ByName("main.App.DeleteCodexCredentialGroupByID", arg1);
}

export function SetDefaultCodexCredentialGroup(arg1) {
  return Call.ByName("main.App.SetDefaultCodexCredentialGroup", arg1);
}

export function CreateCodexEndpoint(arg1) {
  return Call.ByName("main.App.CreateCodexEndpoint", arg1);
}

export function UpdateCodexEndpointByID(arg1, arg2) {
  return Call.ByName("main.App.UpdateCodexEndpointByID", arg1, arg2);
}

export function DeleteCodexEndpointByID(arg1) {
  return Call.ByName("main.App.DeleteCodexEndpointByID", arg1);
}

export function ActivateCodexEndpoint(arg1) {
  return Call.ByName("main.App.ActivateCodexEndpoint", arg1);
}

export function GetCodexSuspendedCount() {
  return Call.ByName("main.App.GetCodexSuspendedCount");
}

export function GetCodexGlobalConfig() {
  return Call.ByName("main.App.GetCodexGlobalConfig");
}

export function UpdateCodexGlobalConfig(arg1) {
  return Call.ByName("main.App.UpdateCodexGlobalConfig", arg1);
}
