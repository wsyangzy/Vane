import test from 'node:test';
import assert from 'node:assert/strict';
import { filterQuotaHealthChannels, healthGroupSummary, quotaGroupSummary, quotaHealthChannels } from './quotaHealthState.js';

test('quota summary separates current usage from stale or unknown windows without inventing an overall quota', () => {
  const now = Date.parse('2026-10-26T12:00:00Z');
  const current = { expiresAt: '2026-10-26T13:00:00Z', confidence: 'exact' };
  const result = quotaGroupSummary([
    { ...current, modelScope: '', usedPercent: 21 },
    { ...current, modelScope: 'gpt-special', usedPercent: 85 },
    { ...current, expiresAt: '2026-10-26T11:00:00Z', usedPercent: 100 },
    { ...current, confidence: 'unknown', usedPercent: null, error: 'unavailable' },
  ], now);
  assert.equal(result.label, '最高使用 85.0%');
  assert.equal(result.description, '4 个窗口 · 2 个模型范围');
  assert.equal(result.tone, 'warning');
  assert.equal(result.issue, '1 个查询失败 · 1 个历史快照 · 1 个额度未知');
  assert.equal(quotaGroupSummary([{ ...current, usedPercent: 0 }], now).label, '最高使用 0.0%');
  assert.equal(quotaGroupSummary([{ ...current, usedPercent: 100 }], now).tone, 'danger');
  assert.equal(quotaGroupSummary([{ ...current, confidence: 'unknown', usedPercent: null }], now).label, '额度待确认');
  assert.equal(quotaGroupSummary([], now).label, '待查询');
});

test('health summary keeps hidden endpoint failures visible and uses the strongest failure tone', () => {
  const candidates = [
    { enabled: true, health: {} },
    { enabled: true, health: { coolingDown: true, reason: 'rate_limit' } },
    { enabled: true, health: { repairRequired: true, reason: 'credential_rejected' } },
  ];
  const summary = healthGroupSummary(candidates);
  assert.equal(summary.label, '2 个需关注');
  assert.equal(summary.tone, 'danger');
  assert.match(summary.reason, /上游拒绝凭证/);
  assert.deepEqual(healthGroupSummary([candidates[0]]), { label: '全部正常', tone: 'success', reason: '' });
});

test('relay groups remain visible without fabricated quota and manual balances stay group scoped', () => {
  const balances = [{ channelId: 1, groupId: 2, enabled: true, balanceUsd: '0.000000001' }];
  const resources = quotaHealthChannels([{ id: 1, name: '中转' }], [{ channelId: 1, groupId: 2, groupName: '余额分组' }, { channelId: 1, groupId: 3, groupName: '无额度分组' }], [], [], [], balances);
  const groups = filterQuotaHealthChannels(resources, 'quotas', '')[0].groups;
  assert.equal(groups.length, 2);
  assert.equal(groups[0].balance.balanceUsd, '0.000000001');
  assert.equal(groups[1].balance, null);
  assert.ok(groups.every((group) => group.quotas.length === 0));
  assert.equal(filterQuotaHealthChannels(resources, 'quotas', '本地估算')[0].groups[0].id, 2);
  assert.equal(filterQuotaHealthChannels(resources, 'quotas', '未提供额度')[0].groups[0].id, 3);
});

test('subscription scope includes unqueried subscriptions and excludes relay groups within the same channel', () => {
  const resources = quotaHealthChannels([{ id: 1, name: '混合渠道' }, { id: 2, name: '中转渠道' }], [
    { channelId: 1, groupId: 1, groupName: '已查询订阅', managed: true },
    { channelId: 1, groupId: 2, groupName: '未查询订阅', managed: true },
    { channelId: 1, groupId: 3, groupName: '中转凭证' },
    { channelId: 2, groupId: 1, groupName: '已设置余额' },
  ], [{ channelId: 1, groupId: 1, modelScope: '', windowName: '5h' }], [], [], [{ channelId: 2, groupId: 1, enabled: true, balanceUsd: '20' }]);
  const subscriptionScope = { subscriptionsOnly: true };
  const subscriptions = filterQuotaHealthChannels(resources, 'quotas', '', subscriptionScope);
  assert.deepEqual(subscriptions.map((channel) => [channel.id, channel.groups.map((group) => group.id)]), [[1, [1, 2]]]);
  assert.equal(subscriptions[0].groups[1].quotas.length, 0);
  assert.equal(filterQuotaHealthChannels(resources, 'quotas', '混合渠道', subscriptionScope)[0].groups.length, 2);
  assert.equal(filterQuotaHealthChannels(resources, 'quotas', '中转', subscriptionScope).length, 0);
  assert.equal(filterQuotaHealthChannels(resources, 'quotas', '').flatMap((channel) => channel.groups).length, 4);
  assert.equal(resources[0].groups.length, 3);
});

test('anomaly scope excludes healthy endpoints without hiding failures or affecting the full view', () => {
  const candidates = [
    { channelId: 1, groupId: 1, endpointId: 'ok', enabled: true },
    { channelId: 1, groupId: 1, endpointId: 'cooling', enabled: true, health: { coolingDown: true } },
    { channelId: 1, groupId: 2, endpointId: 'repair', enabled: true, health: { repairRequired: true } },
    { channelId: 2, groupId: 1, endpointId: 'normal', enabled: true },
  ];
  const resources = quotaHealthChannels([{ id: 1, name: '混合渠道' }, { id: 2, name: '正常渠道' }], [], [], candidates);
  const issues = filterQuotaHealthChannels(resources, 'candidates', '', { issuesOnly: true });
  assert.deepEqual(issues.map((channel) => [channel.id, channel.groups.flatMap((group) => group.candidates.map((candidate) => candidate.endpointId))]), [[1, ['cooling', 'repair']]]);
  assert.equal(filterQuotaHealthChannels(resources, 'candidates', '正常渠道', { issuesOnly: true }).length, 0);
  assert.equal(filterQuotaHealthChannels(resources, 'candidates', '').flatMap((channel) => channel.groups.flatMap((group) => group.candidates)).length, 4);
  assert.equal(resources[0].groups[0].candidates.length, 2);
});

test('quota and health group by channel and group IDs while keeping every model window and endpoint', () => {
  const quotas = [
    { channelId: 1, groupId: 11, modelScope: '', windowName: '5h', usedPercent: 20 },
    { channelId: 1, groupId: 11, modelScope: 'claude-opus', windowName: '5h', usedPercent: 80 },
  ];
  const candidates = ['a', 'b'].map((endpointId) => ({ channelId: 1, groupId: 11, endpointId, enabled: true, load: { concurrent: 2, decayedTokens: 100 } }));
  const result = quotaHealthChannels([{ id: 2, name: '第二渠道' }, { id: 1, name: '第一渠道' }], [{ channelId: 1, groupId: 11, groupName: '同名分组' }, { channelId: 2, groupId: 11, groupName: '同名分组', managed: true }], quotas, candidates);
  assert.deepEqual(result.map((channel) => channel.id), [2, 1]);
  assert.equal(result[0].groups[0].quotas.length, 0);
  assert.equal(result[0].groups[0].managed, true);
  assert.deepEqual(result[1].groups[0].quotas.map((quota) => quota.usedPercent), [20, 80]);
  assert.equal(result[1].groups[0].candidates.length, 2);
  assert.equal(result[1].groups[0].candidates[0].load.concurrent, 2);
  assert.equal(candidates[1].load.decayedTokens, 100);
});

test('resource filtering matches parent names and individual models or endpoints without mutating source', () => {
  const result = quotaHealthChannels([{ id: 1, name: 'AnyRouter' }], [{ channelId: 1, groupId: 1, groupName: '默认分组', managed: true }], [
    { channelId: 1, groupId: 1, modelScope: '', windowName: '5h' },
    { channelId: 1, groupId: 1, modelScope: 'claude-opus', windowName: 'week' },
  ], [
    { channelId: 1, groupId: 1, endpointId: 'healthy', enabled: true },
    { channelId: 1, groupId: 1, endpointId: 'backup', enabled: true, health: { coolingDown: true, reason: 'account_cooling_down' } },
  ]);
  assert.equal(filterQuotaHealthChannels(result, 'quotas', ' ANYROUTER ')[0].groups[0].quotas.length, 2);
  assert.equal(filterQuotaHealthChannels(result, 'quotas', 'opus')[0].groups[0].quotas.length, 1);
  assert.deepEqual(filterQuotaHealthChannels(result, 'candidates', '', { issuesOnly: true })[0].groups[0].candidates.map((candidate) => candidate.endpointId), ['backup']);
  assert.equal(filterQuotaHealthChannels(result, 'candidates', 'healthy', { issuesOnly: true }).length, 0);
  assert.equal(filterQuotaHealthChannels(result, 'quotas', 'missing').length, 0);
  assert.equal(result[0].groups[0].quotas.length, 2);
  assert.equal(result[0].groups[0].candidates.length, 2);
});

test('snapshots with removed resources remain visible with safe identity fallbacks', () => {
  const result = quotaHealthChannels([], [], [{ channelId: 9, groupId: 4, modelScope: '', windowName: 'week' }], []);
  assert.equal(result[0].name, '渠道 9');
  assert.equal(result[0].groups[0].name, '分组 4');
  assert.equal(filterQuotaHealthChannels(result, 'quotas', '').length, 1);
  assert.equal(filterQuotaHealthChannels(result, 'candidates', '').length, 0);
});

test('health resolves numeric and compound endpoint identities to resource names within the exact channel and group', () => {
  const channels = [{ id: 1, name: 'AnyRouter' }, { id: 2, name: 'Right Code' }];
  const candidates = [
    { channelId: 1, groupId: 11, endpointId: '10', enabled: true },
    { channelId: 1, groupId: 12, endpointId: '10', enabled: true },
    { channelId: 2, groupId: 19, endpointId: 'Right Code::g19::awsq', enabled: true },
  ];
  const endpoints = [
    { channelId: '1', groupId: '11', endpointId: 10, name: '主线路', address: 'https://primary.example/v1/' },
    { channelId: 1, groupId: 12, endpointId: '10', name: '备用线路', address: 'https://backup.example/v1' },
    { channelId: 2, groupId: 19, endpointId: 'Right Code::g19::awsq', name: 'awsq', address: 'https://aws.example/api' },
  ];
  const result = quotaHealthChannels(channels, [], [], candidates, endpoints);
  assert.deepEqual(result.flatMap((channel) => channel.groups.flatMap((group) => group.candidates.map((candidate) => [candidate.endpointName, candidate.endpointAddress]))), [
    ['主线路', 'https://primary.example/v1'], ['备用线路', 'https://backup.example/v1'], ['awsq', 'https://aws.example/api'],
  ]);
  assert.equal(filterQuotaHealthChannels(result, 'candidates', '主线路')[0].groups[0].candidates[0].endpointId, '10');
  assert.equal(filterQuotaHealthChannels(result, 'candidates', 'AWS.EXAMPLE')[0].id, 2);
  assert.equal(Object.hasOwn(candidates[0], 'endpointName'), false);
});

test('endpoint display and search omit URL credentials, query parameters and fragments', () => {
  const result = quotaHealthChannels([], [], [], [{ channelId: 1, groupId: 1, endpointId: '10', enabled: true }], [
    { channelId: 1, groupId: 1, endpointId: '10', name: '主线路', address: 'https://private-user:private-password@api.example:8443/v1?key=private-key#private-fragment' },
  ]);
  assert.equal(result[0].groups[0].candidates[0].endpointAddress, 'https://api.example:8443/v1');
  assert.equal(filterQuotaHealthChannels(result, 'candidates', 'private-').length, 0);
});

test('missing endpoint resources and invalid addresses keep the candidate without inventing a display name', () => {
  const result = quotaHealthChannels([], [], [], [
    { channelId: 1, groupId: 1, endpointId: '10', enabled: true },
    { channelId: 1, groupId: 1, endpointId: '11', enabled: true },
    { channelId: 1, groupId: 1, endpointId: '12', enabled: true },
  ], [
    { channelId: 1, groupId: 1, endpointId: '11', name: '不可用地址', address: 'not-a-url?private-key' },
    { channelId: 1, groupId: 1, endpointId: '12', name: '其他协议', address: 'file:///private-path' },
  ]);
  assert.equal(result[0].groups[0].candidates.length, 3);
  assert.equal(result[0].groups[0].candidates[0].endpointName, '');
  assert.ok(result[0].groups[0].candidates.every((candidate) => candidate.endpointAddress === ''));
});
