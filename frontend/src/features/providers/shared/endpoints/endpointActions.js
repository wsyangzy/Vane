export const selectPersistedEndpoints = (endpoints = []) => (
  endpoints.filter((endpoint) => endpoint?.id)
);

export const buildCodexEndpointMutationPayload = (endpoint, overrides = {}) => ({
  name: endpoint?.name || '',
  baseURL: endpoint?.baseURL || endpoint?.base_url || '',
  credentialGroupId: endpoint?.credentialGroupId || endpoint?.credential_group_id || 0,
  priority: endpoint?.priority,
  timeoutSeconds: endpoint?.timeoutSeconds ?? endpoint?.timeout_seconds ?? 0,
  ...overrides
});
