import test from 'node:test';
import assert from 'node:assert/strict';
import { describeBlockageRecovery, mapBlockage } from './blockage.js';
import { buildFailureToast, getRequestFailureDetails } from './failureSummary.js';
import { createFailureNotificationAggregator } from './failureNotifications.js';

const at = Date.parse('2026-11-17T11:00:00+08:00');
const limit = { provider: 'codex', channel_id: 38, group_id: 5, endpoint_id: '97', channel_name: 'ZeroCat', group_name: '混合池',
  endpoint_name: '主站', model: 'model', reason: 'daily_cost_limit_exceeded', recovery: 'automatic',
  recover_at: '2026-11-18T00:00:00+08:00', daily_used_usd: 5.125, daily_limit_usd: 5, observed_at: '2026-11-17T10:53:31+08:00' };

test('daily limit recovery retains exact saved amounts and describes a real reset without claiming upstream health', () => {
  const mapped = mapBlockage(limit);
  assert.equal(mapped.groupId, 5);
  assert.equal(mapped.dailyUsedUsd, 5.125);
  const view = describeBlockageRecovery(mapped, at);
  assert.equal(view.costs, '今日已用 $5.1250 / 限额 $5.0000');
  assert.match(view.recovery, /限额重置/);
  assert.match(view.action, /编辑此凭证分组/);
  const old = describeBlockageRecovery(limit, at + 86400000);
  assert.match(old.recovery, /这不代表上游已恢复/);
});

test('unknown and permanent blocks never invent automatic recovery times', () => {
  const old = describeBlockageRecovery({ reason: 'daily_cost_limit_exceeded' }, at);
  assert.match(old.costs, /未保存已用金额/);
  assert.match(old.recovery, /未保存准确时刻/);
  const credential = describeBlockageRecovery({ reason: 'credential_repair_required', recovery: 'manual' }, at);
  assert.match(credential.recovery, /单纯等待不会解除/);
  assert.match(credential.action, /更新此分组的凭证/);
  const unknown = describeBlockageRecovery({ reason: 'unrecognized_reason' }, at);
  assert.match(unknown.recovery, /恢复时间未知/);
});

test('every blocker appears in mixed upstream failure notifications with its saved group, amounts and remedy', () => {
  const failure = { provider: 'codex', request_id: 'failed', failure_reason: 'incomplete_stream', http_status_is_upstream: true, http_status_code: 200,
    endpoint_chain: [{ channel: 'NewCodex', endpoint: '默认', attempts: 1, status_code: 200, error: '<html>upstream-body</html>' }],
    route_snapshot: { profile_id: 2, attempts: 1, candidates: [
      { action: 'skipped', reason: limit.reason, candidate: { channel: '38', endpoint: '97', model: 'model' }, blockage: limit },
      { action: 'skipped', reason: limit.reason, candidate: { channel: '38', endpoint: '97', model: 'model' }, blockage: limit },
    ] } };
  const details = getRequestFailureDetails(failure);
  assert.equal(details.blockingCandidates.length, 1);
  assert.equal(details.blockingCandidates[0].count, 1);
  assert.equal(details.blockingCandidates[0].channel, 'ZeroCat');
  assert.equal(details.blockingCandidates[0].credentialGroup, '混合池');
  const toast = buildFailureToast(failure);
  const message = toast.message.join('\n');
  assert.match(message, /ZeroCat \/ 混合池/);
  assert.match(message, /\$5\.1250.*\$5\.0000/);
  assert.match(message, /限额重置|记录的等待时间已到/);
  assert.match(message, /编辑此凭证分组/);
  assert.ok(!message.includes('upstream-body'));
  assert.equal(createFailureNotificationAggregator().consume([failure], at).length, 1);
});

test('the notification merge keeps different groups or triggers separate while updating changed cost figures', () => {
  const aggregator = createFailureNotificationAggregator();
  const request = (blockage) => ({ provider: 'codex', failure_reason: 'recovery_outside_budget', dispatchTrace: {
    profileId: 2, attempts: [], skipped: [{ channel: 'ZeroCat', reason: blockage.reason, blockage }],
  } });
  const first = aggregator.consume([request(limit)], at)[0];
  const next = aggregator.consume([request({ ...limit, daily_used_usd: 6 })], at + 1000)[0];
  assert.equal(first.id, next.id);
  assert.equal(next.update, true);
  assert.match(next.message.join('\n'), /\$6\.0000/);
  const other = aggregator.consume([request({ ...limit, group_id: 6, group_name: '其他分组' })], at + 2000)[0];
  assert.notEqual(first.id, other.id);
  const trigger = aggregator.consume([request({ ...limit, cause: 'network_error' })], at + 3000)[0];
  assert.notEqual(first.id, trigger.id);
});

test('an empty resolved trace does not discard a direct daily cost rejection', () => {
  const details = getRequestFailureDetails({ failure_reason: 'daily_cost_limit_exceeded', channel: 'ZeroCat', group_name: '常用', dispatchTrace: { skipped: [], attempts: [] } });
  assert.equal(details.blockingCandidates[0].reason, 'daily_cost_limit_exceeded');
  assert.match(details.blockingCandidates[0].recovery.action, /编辑此凭证分组/);
});

test('recovery stop shows its recorded request limits beside the actual blocker', () => {
  const toast = buildFailureToast({ provider: 'codex', failure_reason: 'recovery_outside_budget', http_status_is_upstream: false,
    route_snapshot: { attempts: 0, max_attempts: 10, max_duration_ms: 60000, next_recovery_at: limit.recover_at,
      candidates: [{ action: 'skipped', reason: limit.reason, candidate: { channel: '38', endpoint: '97' }, blockage: limit }] } });
  assert.match(toast.message.join('\n'), /实际向上游发送 0 次/);
  assert.match(toast.message.join('\n'), /最多发送 10 次，总时长上限 60 秒/);
  assert.match(toast.message.join('\n'), /每日成本限额/);
});
