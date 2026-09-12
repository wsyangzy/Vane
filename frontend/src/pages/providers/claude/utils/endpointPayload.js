export const buildClaudeEndpointCopyPayload = (endpoint, channel, credentialGroupId) => ({
  channel,
  credentialGroupId,
  name: endpoint?.name || '',
  url: endpoint?.url || endpoint?.baseURL || endpoint?.base_url || '',
  headers: endpoint?.headers || {},
  priority: endpoint?.priority,
  failoverEnabled: endpoint?.failoverEnabled ?? endpoint?.failover_enabled ?? true,
  skipHealthCheck: endpoint?.skipHealthCheck ?? endpoint?.skip_health_check ?? false,
  cooldownSeconds: endpoint?.cooldownSeconds ?? endpoint?.cooldown_seconds ?? null,
  timeoutSeconds: endpoint?.timeoutSeconds ?? endpoint?.timeout_seconds ?? 300,
  supportsCountTokens: endpoint?.supportsCountTokens ?? endpoint?.supports_count_tokens ?? false
});
