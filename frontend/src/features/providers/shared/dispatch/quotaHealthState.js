import { candidateHealthPresentation, quotaPresentation } from './dispatchState.js';

export function healthGroupSummary(candidates) {
  const states = candidates.map(candidateHealthPresentation);
  const issues = states.filter((state) => state.tone !== 'success');
  const worst = ['danger', 'warning', 'muted'].map((tone) => issues.find((state) => state.tone === tone)).find(Boolean);
  return { tone: worst?.tone || 'success', label: issues.length ? `${issues.length} 个需关注` : '全部正常', reason: [...new Set(issues.map((state) => state.reason).filter(Boolean))].join('；') };
}

export function quotaGroupSummary(quotas, now = Date.now()) {
  if (!quotas.length) return { label: '待查询', description: '托管订阅', tone: 'muted', issue: '' };
  const states = quotas.map((quota) => quotaPresentation(quota, now));
  const current = quotas.filter((quota, index) => states[index].known);
  const maximum = current.length ? Math.max(...current.map((quota) => Number(quota.usedPercent))) : null;
  const stale = states.filter((state) => state.stale).length;
  const unknown = states.filter((state) => !state.known && !state.stale).length;
  const errors = quotas.filter((quota) => quota.error).length;
  const issues = [errors && `${errors} 个查询失败`, stale && `${stale} 个历史快照`, unknown && `${unknown} 个额度未知`].filter(Boolean);
  return {
    label: maximum === null ? '额度待确认' : `最高使用 ${maximum.toFixed(1)}%`,
    description: `${quotas.length} 个窗口 · ${new Set(quotas.map((quota) => quota.modelScope || '')).size} 个模型范围`,
    tone: maximum >= 100 ? 'danger' : maximum >= 80 || issues.length ? 'warning' : maximum === null ? 'muted' : 'default',
    issue: issues.join(' · '),
  };
}

function endpointAddress(value) {
  try {
    const url = new URL(value);
    if (!['http:', 'https:'].includes(url.protocol)) return '';
    return `${url.protocol}//${url.host}${url.pathname.replace(/\/$/, '')}`;
  } catch { return ''; }
}

const endpointKey = (value) => JSON.stringify([Number(value.channelId), Number(value.groupId), String(value.endpointId)]);

export function quotaHealthChannels(channels, accounts, quotas, candidates, endpoints = [], balances = []) {
  const result = new Map();
  const endpointDetails = new Map(endpoints.map((endpoint) => [endpointKey(endpoint), endpoint]));
  const groupFor = (channelId, groupId) => {
    if (!result.has(channelId)) result.set(channelId, {
      id: channelId,
      name: channels.find((channel) => channel.id === channelId)?.name || accounts.find((account) => account.channelId === channelId)?.channelName || `渠道 ${channelId}`,
      groups: new Map(),
    });
    const channel = result.get(channelId);
    if (!channel.groups.has(groupId)) {
      const account = accounts.find((item) => item.channelId === channelId && item.groupId === groupId);
      channel.groups.set(groupId, { id: groupId, name: account?.groupName || `分组 ${groupId}`, managed: Boolean(account?.managed), passthrough: account?.authMode === 'client_passthrough', balance: balances.find((balance) => balance.channelId === channelId && balance.groupId === groupId) || null, quotas: [], candidates: [] });
    }
    return channel.groups.get(groupId);
  };
  // All groups have an explicit quota capability, including 'not provided'.
  for (const account of accounts) groupFor(account.channelId, account.groupId);
  for (const balance of balances) groupFor(balance.channelId, balance.groupId);
  for (const quota of quotas) groupFor(quota.channelId, quota.groupId).quotas.push(quota);
  for (const candidate of candidates) {
    const endpoint = endpointDetails.get(endpointKey(candidate));
    groupFor(candidate.channelId, candidate.groupId).candidates.push({
      ...candidate,
      endpointName: endpoint?.name?.trim() || '',
      endpointAddress: endpointAddress(endpoint?.address),
    });
  }
  const order = new Map(channels.map((channel, index) => [channel.id, index]));
  return [...result.values()].sort((a, b) => (order.get(a.id) ?? Infinity) - (order.get(b.id) ?? Infinity)).map((channel) => ({ ...channel, groups: [...channel.groups.values()] }));
}

export function filterQuotaHealthChannels(channels, kind, query, { issuesOnly = false, subscriptionsOnly = false } = {}) {
  const term = query.trim().toLocaleLowerCase();
  const matches = (...values) => values.some((value) => String(value || '').toLocaleLowerCase().includes(term));
  return channels.map((channel) => ({ ...channel, groups: channel.groups.flatMap((group) => {
    const parentMatches = matches(channel.name, group.name);
    if (kind === 'quotas') {
      if (subscriptionsOnly && !group.managed) return [];
      const quotas = group.quotas.filter((quota) => parentMatches || matches(quota.modelScope, quota.windowName, quota.source, quota.error));
      const status = group.managed ? '订阅额度 官方额度' : group.balance?.enabled ? '手动余额 本地估算' : '未提供额度';
      return quotas.length || parentMatches || matches(status) ? [{ ...group, quotas }] : [];
    }
    const candidates = group.candidates.filter((candidate) => {
      const health = candidateHealthPresentation(candidate);
      return (!issuesOnly || health.tone !== 'success') && (parentMatches || matches(candidate.endpointName, candidate.endpointAddress, candidate.endpointId, candidate.model, candidate.upstreamModel, candidate.protocol, health.label, health.reason));
    });
    return candidates.length ? [{ ...group, candidates }] : [];
  }) })).filter((channel) => channel.groups.length);
}
