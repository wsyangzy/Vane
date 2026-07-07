export const getCredentialGroupForEndpoint = (credentialGroups = [], endpoint = null) => {
  const endpointGroupID = Number(endpoint?.credentialGroupId || endpoint?.credential_group_id || 0);
  const defaultGroup = credentialGroups.find((group) => group?.isDefault || group?.is_default) || null;
  if (endpointGroupID > 0) {
    return credentialGroups.find((group) => Number(group?.id) === endpointGroupID) || defaultGroup;
  }
  return defaultGroup || credentialGroups[0] || null;
};

export const getCredentialGroupCostMultiplier = (credentialGroup, endpoint = null) => (
  credentialGroup?.costMultiplier ??
  credentialGroup?.cost_multiplier ??
  endpoint?.costMultiplier ??
  endpoint?.cost_multiplier ??
  1
);
