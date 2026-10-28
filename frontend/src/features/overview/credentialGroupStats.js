const groupKey = (channelId, groupId) => JSON.stringify([Number(channelId), Number(groupId)]);

export const groupStateOptions = [
  { key: 'available', label: '可用', color: '#10b981' },
  { key: 'partial', label: '部分可用', color: '#f59e0b' },
  { key: 'blocked', label: '暂不可用', color: '#f43f5e' },
  { key: 'unconfigured', label: '待配置', color: '#94a3b8' },
];

// Health is independent of profile membership, balance and subscription quota.
// A credential group remains usable while any configured candidate is usable.
export function summarizeCredentialGroups(workspace) {
  const channels = new Map((workspace.channels || []).map((channel) => [Number(channel.id), { ...channel, groups: [], counts: Object.fromEntries(groupStateOptions.map(({ key }) => [key, 0])) }]));
  const groups = new Map();
  for (const account of workspace.accounts || []) {
    const key = groupKey(account.channelId, account.groupId);
    if (groups.has(key)) continue;
    if (!channels.has(Number(account.channelId))) channels.set(Number(account.channelId), { id: account.channelId, name: account.channelName || `渠道 ${account.channelId}`, groups: [], counts: Object.fromEntries(groupStateOptions.map(({ key }) => [key, 0])) });
    const group = { id: account.groupId, name: account.groupName || `分组 ${account.groupId}`, candidates: new Map() };
    groups.set(key, group);
    channels.get(Number(account.channelId)).groups.push(group);
  }
  for (const candidate of workspace.candidates || []) {
    const group = groups.get(groupKey(candidate.channelId, candidate.groupId));
    if (!group) continue;
    const key = JSON.stringify([candidate.endpointId, candidate.model || '', candidate.upstreamModel || '', candidate.protocol || '']);
    group.candidates.set(key, candidate);
  }
  const counts = Object.fromEntries(groupStateOptions.map(({ key }) => [key, 0]));
  for (const channel of channels.values()) {
    for (const group of channel.groups) {
      const candidates = [...group.candidates.values()];
      const usable = candidates.filter((candidate) => candidate.enabled && !candidate.health?.repairRequired && !candidate.health?.coolingDown && !['account_disabled', 'account_missing'].includes(candidate.health?.reason));
      group.state = usable.length ? usable.length === candidates.length ? 'available' : 'partial'
        : candidates.some((candidate) => candidate.enabled || candidate.health?.repairRequired || candidate.health?.coolingDown || candidate.health?.reason === 'account_missing') ? 'blocked' : 'unconfigured';
      delete group.candidates;
      channel.counts[group.state]++;
      counts[group.state]++;
    }
  }
  return { channels: [...channels.values()], total: groups.size, usable: counts.available + counts.partial, counts };
}

export function prepareGroupCosts(items) {
  return (items || []).map((item) => ({ ...item, name: `${item.provider === 'codex' ? 'Codex' : 'Claude'} · ${item.channel || '未记录渠道'} / ${item.group || '未记录分组'}` }));
}
