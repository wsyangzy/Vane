import test from 'node:test';

import { describeRouteExecution } from './routeTargets.js';
import assert from 'node:assert/strict';

import {
  buildNonStreamRouteForm,
  describeRouteSnapshot,
  moveRouteTarget,
  nonStreamRouteFromForm,
  nonStreamRouteSignature,
  routeChannelLabel,
  validateModelRouteForm,
  validateRouteTargets
} from './routeTargets.js';

test('model route form validation reports the first missing field', () => {
  const rule = { name: 'haiku', matchModel: 'claude-haiku-*', priority: '0', targets: [{ channelId: 3 }] };
  assert.equal(validateModelRouteForm(rule), '');
  assert.equal(validateModelRouteForm({ ...rule, name: ' ' }), '请填写规则名称');
  assert.equal(validateModelRouteForm({ ...rule, matchModel: '' }), '请填写匹配模型');
  assert.equal(validateModelRouteForm({ ...rule, priority: '1.5' }), '优先级必须是整数');
  assert.equal(validateModelRouteForm({ ...rule, targets: [{ channelId: 0 }] }), '请至少选择一个目标渠道');
});

test('an enabled non-stream route without any channel is rejected before saving', () => {
  // 后端把空目标视为关闭：不拦的话开关看似打开、实际没有生效
  const form = { ...buildNonStreamRouteForm(null), enabled: true };
  assert.equal(validateRouteTargets(form.targets), '请至少选择一个目标渠道');
  assert.deepEqual(nonStreamRouteFromForm(form), { mode: 'prefer', targets: [] });

  const filled = { ...form, targets: [{ channelId: 3, groupId: 0, upstreamModel: '' }, { channelId: 0, groupId: 0, upstreamModel: '' }] };
  assert.equal(validateRouteTargets(filled.targets), '');
  assert.deepEqual(nonStreamRouteFromForm(filled).targets, [{ channelId: 3, groupId: 0, upstreamModel: '' }]);
});

test('turning the switch off clears the route regardless of leftover rows', () => {
  const form = buildNonStreamRouteForm({ mode: 'fallback', targets: [{ channelId: 2, groupId: 5, upstreamModel: 'm' }] });
  assert.equal(form.enabled, true);
  assert.equal(form.mode, 'fallback');
  assert.equal(nonStreamRouteFromForm({ ...form, enabled: false }), null);
});

test('route signature only changes when submitted fields change', () => {
  const saved = {
    mode: 'prefer',
    targets: [{ channelId: 7, groupId: 0, upstreamModel: 'deepseek-chat', channelName: 'deepseek', missing: false }]
  };
  // 展示字段、首尾空白与空行不算变更
  assert.equal(
    nonStreamRouteSignature({ mode: 'prefer', targets: [{ channelId: 7, groupId: 0, upstreamModel: ' deepseek-chat ' }, { channelId: 0 }] }),
    nonStreamRouteSignature(saved)
  );
  assert.notEqual(nonStreamRouteSignature({ ...saved, mode: 'fallback' }), nonStreamRouteSignature(saved));
  assert.notEqual(
    nonStreamRouteSignature({ mode: 'prefer', targets: [{ channelId: 7, groupId: 4, upstreamModel: 'deepseek-chat' }] }),
    nonStreamRouteSignature(saved)
  );
  // 关闭与「没有目标」等价
  assert.equal(nonStreamRouteSignature(null), nonStreamRouteSignature({ mode: 'prefer', targets: [] }));
});

test('moving a target swaps neighbours and ignores out-of-range moves', () => {
  const targets = [{ channelId: 1 }, { channelId: 2 }, { channelId: 3 }];
  assert.deepEqual(moveRouteTarget(targets, 2, -1).map((t) => t.channelId), [1, 3, 2]);
  assert.deepEqual(moveRouteTarget(targets, 0, -1).map((t) => t.channelId), [1, 2, 3]);
  assert.deepEqual(moveRouteTarget(targets, 2, 1).map((t) => t.channelId), [1, 2, 3]);
  // 不修改入参
  assert.deepEqual(targets.map((t) => t.channelId), [1, 2, 3]);
});

test('channel labels surface the states that affect routing', () => {
  assert.equal(routeChannelLabel({ name: 'a' }), 'a');
  assert.equal(routeChannelLabel({ name: 'anyrouter', active: true, paused: true, nonStreamRouteEnabled: true }), 'anyrouter（当前 · 不接非流）');
  assert.equal(routeChannelLabel({ name: 'b', paused: true }), 'b（已暂停）');
});

test('route snapshots describe the rule, the mode and the stage that served the request', () => {
  assert.deepEqual(
    describeRouteSnapshot('{"kind":"model_rule","id":3,"name":"haiku 改道","mode":"prefer","target":2}'),
    { kind: 'model_rule', title: '模型规则「haiku 改道」', mode: '优先目标', stage: '第 2 个目标' }
  );
  // 规则按渠道隔离后，快照带来源渠道
  assert.equal(
    describeRouteSnapshot({ kind: 'model_rule', name: 'haiku 改道', channel: 'anyrouter', target: 1 }).title,
    '模型规则「haiku 改道」（anyrouter）'
  );
  assert.deepEqual(
    describeRouteSnapshot({ kind: 'non_stream', channel: 'anyrouter', mode: 'fallback', target: 0 }),
    { kind: 'non_stream', title: '非流改道（来自 anyrouter）', mode: '失败后改道', stage: '来源渠道' }
  );
  const capability = describeRouteSnapshot('{"kind":"model_capability","channel":"anyrouter","target":1}');
  assert.equal(capability.stage, '第 1 个候选渠道');
  assert.equal(capability.mode, '');

  for (const raw of ['', '  ', 'not json', '{}', null, undefined]) {
    assert.equal(describeRouteSnapshot(raw), null);
  }
});

test('execution trace preserves model/scope and keeps old records unknown', () => {
  assert.equal(describeRouteExecution('{"kind":"model_rule","target":1}'), null);
  assert.equal(describeRouteExecution('broken'), null);
  const trace = describeRouteExecution(JSON.stringify({ attempts: 2, wait_ms: 15, stop_reason: 'retry_budget_exhausted', execution: { dropped_events: 3, events: [{ candidate: { channel: 'B', model: 'X' }, action: 'failed', reason: 'model_not_found', failure_scope: 'model' }] } }));
  assert.deepEqual(trace, { subscription: null, attempts: 2, waitMs: 15, stopReason: 'retry_budget_exhausted', dropped: 3, events: [{ channel: 'B', model: 'X', action: 'failed', reason: 'model_not_found', scope: 'model' }] });
});


test('subscription execution exposes public binding and preserves uncertain recovery', () => {
  const snapshot = { kind: 'subscription', channel: 'official', subscription: { group_id: 7, binding_version: 4, official_enabled: false, availability: [{ model: 'family:opus', state: 'due_unverified', reason: 'quota_exhausted', source: 'observed-header', resume_not_before: '2026-10-16T12:00:00Z', reset_known: false }] }, execution: { events: [] } };
  const execution = describeRouteExecution(snapshot);
  assert.equal(execution.subscription.groupId, 7);
  assert.equal(execution.subscription.officialEnabled, false);
  assert.equal(execution.subscription.availability[0].state, 'due_unverified');
  assert.equal(execution.subscription.availability[0].resetKnown, false);
  assert.match(describeRouteSnapshot(snapshot).title, /官方订阅与授权备用/);
});
