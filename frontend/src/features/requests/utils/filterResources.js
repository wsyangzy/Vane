const names = (values) => [...new Set(values.filter(Boolean))].sort((a, b) => a.localeCompare(b));

// Each group stays under its provider and channel. Filter state only needs
// these names; credentials and endpoint details do not belong in the tree.
export function buildRequestFilterResources(claudeChannels = [], codexChannels = []) {
  return {
    channels: [['claude', 'Claude', claudeChannels], ['codex', 'Codex', codexChannels]].map(([provider, label, channels]) => ({
      provider,
      label,
      channels: names(channels.map(channel => channel.name)).map(name => ({
        name,
        credentialGroups: names(channels.filter(channel => channel.name === name).flatMap(channel => (channel.credentialGroups || []).map(group => group.name)))
      }))
    }))
  };
}

export function requestFilterPath(channels, { provider = '', channel = 'all', group = 'all' }) {
  if (!provider) return '所有渠道';
  const label = channels.find(item => item.provider === provider)?.label || provider;
  if (!channel || channel === 'all') return `${label} / 全部渠道`;
  return `${label} / ${channel} / ${group && group !== 'all' ? group : '全部分组'}`;
}
