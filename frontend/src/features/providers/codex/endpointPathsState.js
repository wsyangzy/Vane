import { endpointPathIdentity, getEndpointPathStrategy } from './pathPolicy.js';

export function groupPathEntries(entries) {
  const channels = new Map();
  for (const entry of entries) {
    if (!channels.has(entry.channel.id)) channels.set(entry.channel.id, { channel: entry.channel, groups: new Map(), entries: [] });
    const channel = channels.get(entry.channel.id);
    const groupId = entry.group?.id ?? 0;
    if (!channel.groups.has(groupId)) channel.groups.set(groupId, { group: entry.group, entries: [] });
    channel.groups.get(groupId).entries.push(entry);
    channel.entries.push(entry);
  }
  return [...channels.values()].map(channel => ({ ...channel, groups: [...channel.groups.values()] }));
}

export const currentPathResult = (endpoint, results) => {
  const row = results?.[endpoint.id];
  return row?.identity === endpointPathIdentity(endpoint) ? row : null;
};

export function pathEntryStatus(entry, row) {
  if (entry.fixed) return { tone: 'muted', label: '官方固定' };
  if (row?.error) return { tone: 'danger', label: row.result ? '应用失败' : '检测失败' };
  if (row?.saved) return { tone: 'success', label: '已应用' };
  if (row?.strategy) return { tone: 'success', label: row.strategy === getEndpointPathStrategy(entry.endpoint) ? '路径已确认' : '可应用' };
  if (!row?.result) return { tone: 'muted', label: '未检测' };
  const candidates = row.result.candidates || [];
  const reason = candidates.length && candidates.every(candidate => candidate.result === candidates[0].result) ? candidates[0].result : '';
  const label = { authentication_failed: '鉴权失败', rate_limited: '上游限流', network_error: '连接失败' }[reason] || '路径待确认';
  return { tone: 'warning', label };
}
