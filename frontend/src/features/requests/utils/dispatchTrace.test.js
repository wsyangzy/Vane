import test from 'node:test';
import assert from 'node:assert/strict';
import { readRequestDispatchTrace, describeRequestRoutePath } from './dispatchTrace.js';
import { getRequestResourcePath } from './endpointChain.js';

const candidate = (provider, channel, group, endpoint) => ({ provider, channel: String(channel), credential: `${channel}/${group}`, endpoint: String(endpoint), model: 'test-model', protocol: provider === 'codex' ? 'responses' : 'messages' });
const resources = [
  { provider: 'codex', id: 1, name: '渠道 A', credentialGroups: [{ id: 11, name: '分组 A' }], endpoints: [{ id: 101, name: '主端点', credentialGroupId: 11 }] },
  { provider: 'codex', id: 2, name: '渠道 B', credentialGroups: [{ id: 21, name: '分组 B' }], endpoints: [{ id: 201, name: '备用端点', credentialGroupId: 21 }] },
  { provider: 'claude', id: 1, name: 'Claude 渠道', credentialGroups: [{ id: 11, name: 'Claude 分组' }] },
];
const trace = (events, extra = {}) => ({ profile_id: 7, profile_name: '请求当时的方案', strategy: 'order', affinity_mode: 'turn', decision: { kind: 'default' }, candidates: events, ...extra });

test('model route tooltip separates model changes from the saved origin and executed destination', () => {
  const request = { provider: 'codex', model: 'gpt-5.6-luna', routeSnapshot: trace([
    { candidate: { ...candidate('codex', 2, 21, 201), model: 'gpt-5.6-luna' }, action: 'attempt' },
  ], { default_source: { channel_id: 1, group_id: 11, channel_name: '渠道 A', group_name: '分组 A' }, decision: { kind: 'model', rule_id: 1, mode: 'prefer', targets: [{ channel_id: 2, group_id: 21, upstream_model: '' }] } }) };
  request.dispatchTrace = readRequestDispatchTrace(request, resources);
  assert.equal(describeRequestRoutePath(request), 'gpt-5.6-luna\n渠道 A / 分组 A → 渠道 B / 分组 B');
  const rewritten = { ...request, model: 'upstream-model', originalModelName: 'requested-model', dispatchTrace: {
    ...request.dispatchTrace, targets: [{ label: '渠道 B / 分组 B', model: 'upstream-model' }], attempts: [{ ...request.dispatchTrace.attempts[0], model: 'upstream-model' }],
  } };
  assert.equal(describeRequestRoutePath(rewritten), 'requested-model → upstream-model\n渠道 A / 分组 A → 渠道 B / 分组 B');
  assert.equal(request.dispatchTrace.attemptCount, 1);
  assert.deepEqual(getRequestResourcePath(request).map(node => node.channel), ['渠道 B']);
  assert.equal(describeRequestRoutePath({}), '');
});

test('route tooltip follows actual attempts instead of unused targets or the old active channel', () => {
  const request = { provider: 'codex', model: 'M', routeSnapshot: trace([
    { candidate: { ...candidate('codex', 1, 11, 101), model: 'M' }, action: 'attempt' },
  ], { decision: { kind: 'non_stream', targets: [{ channel_id: 2, group_id: 0 }, { profile_id: 99 }] } }) };
  request.dispatchTrace = readRequestDispatchTrace(request, resources);
  assert.equal(describeRequestRoutePath(request), 'M\n渠道 A / 分组 A');
  assert.equal(request.dispatchTrace.attempts[0].channel, '渠道 A');
  assert.equal(describeRequestRoutePath({ model: 'M', routeSnapshot: { kind: 'model_rule', channel: 'A' }, endpointChain: ['B::plus::endpoint'] }), 'M\nA → B / plus');
  request.channel = '旧激活渠道';
  request.dispatchTrace.attempts.push({ ...request.dispatchTrace.attempts[0], channel: '渠道 B', credentialGroup: '分组 B', endpoint: '备用端点' });
  assert.equal(describeRequestRoutePath(request), 'M\n渠道 A / 分组 A → 渠道 B / 分组 B');
  request.original_model_name = 'client-model';
  request.model_rewrite_target = 'upstream-model';
  assert.equal(describeRequestRoutePath(request), 'client-model → M\n渠道 A / 分组 A → 渠道 B / 分组 B');
});

test('model rewrite paths use the original model in the persisted dispatch snapshot for both providers', () => {
  for (const provider of ['claude', 'codex']) {
    const request = { provider, model: 'rewritten-model', routeSnapshot: trace([
      { candidate: { ...candidate(provider, 1, 11, 101), model: 'rewritten-model' }, action: 'attempt' },
    ], { requested_model: 'original-model', decision: { kind: 'mapping' } }) };
    request.dispatchTrace = readRequestDispatchTrace(request, resources);
    assert.equal(describeRequestRoutePath(request), provider === 'codex' ? 'original-model → rewritten-model\n渠道 A / 分组 A' : 'original-model → rewritten-model\nClaude 渠道 / Claude 分组');
  }
  assert.equal(describeRequestRoutePath({ model: 'target', originalModelName: 'source' }), 'source → target');
});

test('unsent model rules stay hidden and channel-only retries retain only actual resources', () => {
  const request = { provider: 'codex', model: 'M', routeSnapshot: trace([], { decision: { kind: 'model', targets: [{ channel_id: 2, group_id: 21 }] } }) };
  request.dispatchTrace = readRequestDispatchTrace(request, resources);
  assert.equal(describeRequestRoutePath(request), '');
  const a = { channel: 'A', credentialGroup: 'plus', endpoint: 'first' };
  request.dispatchTrace = { ...request.dispatchTrace, decision: { kind: 'default' }, attemptCount: 3, attempts: [a, { ...a, endpoint: 'second' }, { ...a, channel: 'B' }] };
  assert.equal(describeRequestRoutePath(request), 'M\nA / plus → B / plus');
});

test('Luna routes show the saved origin to pro and exclude the unexecuted second target', () => {
  for (const model of ['gpt-5.6-luna', 'gpt-6-luna']) {
    const c = { ...candidate('codex', 32, 52, 101), model };
    const request = { provider: 'codex', model, channel: '梭子蟹 API', endpoint: 'main',
      endpointChain: [{ channel: '梭子蟹 API', credential_group: 'pro', endpoint: 'main', attempts: 1 }],
      routeSnapshot: trace([{ candidate: c, action: 'attempt' }, { candidate: c, action: 'success' }], {
        profile_name: '默认', requested_model: model, successful: true, attempts: 1,
        default_source: { channel_id: 32, group_id: 41, channel_name: '梭子蟹 API', group_name: '不降智' },
        decision: { kind: 'model', targets: [{ channel_id: 32, group_id: 52 }, { channel_id: 14, group_id: 10 }] },
      }),
    };
    request.dispatchTrace = readRequestDispatchTrace(request);
    assert.equal(describeRequestRoutePath(request), `${model}\n梭子蟹 API / 不降智 → 梭子蟹 API / pro`);
    const withoutOriginalModel = { ...request, dispatchTrace: { ...request.dispatchTrace, requestedModel: '' } };
    assert.equal(describeRequestRoutePath(withoutOriginalModel), `${model}\n梭子蟹 API / 不降智 → 梭子蟹 API / pro`);
  }
  // Legacy snapshots without a saved origin still retain the actual destination.
  assert.equal(describeRequestRoutePath({ model: 'M', routeSnapshot: { kind: 'model_rule' }, endpointChain: ['B::plus::endpoint'] }), 'M\nB / plus');
});

test('model route tooltip omits unused mapped targets when dispatch falls back to another resource', () => {
  const request = { provider: 'codex', model: 'original-model', routeSnapshot: trace([
    { candidate: { ...candidate('codex', 1, 11, 101), model: 'original-model' }, action: 'attempt' },
  ], { requested_model: 'original-model', decision: { kind: 'model', targets: [{ channel_id: 2, group_id: 21, upstream_model: 'target-model' }, { channel_id: 1, group_id: 0 }] } }) };
  request.dispatchTrace = readRequestDispatchTrace(request, resources);
  assert.equal(describeRequestRoutePath(request), 'original-model\n渠道 A / 分组 A');
  assert.equal(request.dispatchTrace.attempts[0].channel, '渠道 A');
  request.dispatchTrace.requestedModel = '';
  assert.equal(describeRequestRoutePath(request), 'original-model\n渠道 A / 分组 A');
});

test('model routes also show a destination channel model override without an explicit rule model', () => {
  const request = { provider: 'codex', model: 'served-model', routeSnapshot: trace([
    { candidate: { ...candidate('codex', 2, 21, 201), model: 'served-model' }, action: 'attempt' },
  ], { requested_model: 'client-model', default_source: { channel_id: 1, group_id: 11 }, decision: { kind: 'model', targets: [{ channel_id: 2, group_id: 21 }] } }) };
  request.dispatchTrace = readRequestDispatchTrace(request, resources);
  assert.equal(describeRequestRoutePath(request), 'client-model → served-model\n渠道 A / 分组 A → 渠道 B / 分组 B');
});

test('default source names survive renamed or deleted resources without becoming actual attempts', () => {
  for (const provider of ['claude', 'codex']) {
    const request = { provider, model: 'M', channel: '无关激活渠道', routeSnapshot: JSON.stringify(trace([], {
      provider, requested_model: 'M', default_source: { channel_id: 1, group_id: 11, channel_name: '历史原渠道', group_name: '历史原分组' },
      decision: { kind: 'model', targets: [{ channel_id: 2, group_id: 21 }] },
    })) };
    for (const currentResources of [resources, []]) {
      request.dispatchTrace = readRequestDispatchTrace(request, currentResources);
      assert.equal(request.dispatchTrace.defaultSource.channel, '历史原渠道');
      assert.equal(request.dispatchTrace.defaultSource.credentialGroup, '历史原分组');
      assert.equal(describeRequestRoutePath(request), '');
      assert.deepEqual(getRequestResourcePath(request), []);
      assert.equal(request.dispatchTrace.attemptCount, 0);
      assert.ok(!describeRequestRoutePath(request).includes('无关激活渠道'));
    }
  }
});

test('default source availability never creates a path before any upstream send', () => {
  const request = { provider: 'codex', model: 'M', routeSnapshot: trace([], {
    defaultSource: { channelId: 1, groupId: 11 }, decision: { kind: 'model', targets: [{ channel_id: 2, group_id: 21 }] },
  }) };
  request.dispatchTrace = readRequestDispatchTrace(request, resources);
  assert.equal(describeRequestRoutePath(request), '');
  request.routeSnapshot.defaultSource = { unavailable: true };
  request.dispatchTrace = readRequestDispatchTrace(request, resources);
  assert.equal(describeRequestRoutePath(request), '');
  delete request.routeSnapshot.defaultSource;
  request.dispatchTrace = readRequestDispatchTrace(request, resources);
  assert.equal(describeRequestRoutePath(request), '');
});

test('saved origins precede actual retries without duplicating the first executed resource', () => {
  for (const provider of ['claude', 'codex']) {
    const a = candidate(provider, 1, 11, 101), b = candidate(provider, 2, 21, 201);
    const request = { provider, model: 'test-model', routeSnapshot: trace([
      { candidate: a, action: 'attempt' }, { candidate: a, action: 'failed' },
      { candidate: b, action: 'attempt' }, { candidate: b, action: 'success' },
    ], {
      default_source: { channel_name: '历史来源', group_name: '来源分组' },
      decision: { kind: 'model', targets: [{ channel_id: 99, group_id: 999 }] },
    }) };
    request.dispatchTrace = readRequestDispatchTrace(request, resources);
    const actualPath = provider === 'codex' ? '渠道 A / 分组 A → 渠道 B / 分组 B' : 'Claude 渠道 / Claude 分组 → 渠道 #2 / 分组 #21';
    assert.equal(describeRequestRoutePath(request), `test-model\n历史来源 / 来源分组 → ${actualPath}`);
    assert.equal(request.dispatchTrace.attemptCount, 2);
    request.dispatchTrace.defaultSource = { ...request.dispatchTrace.attempts[0] };
    assert.equal(describeRequestRoutePath(request), `test-model\n${actualPath}`);
    request.dispatchTrace.defaultSource = { unavailable: true };
    assert.equal(describeRequestRoutePath(request), `test-model\n默认渠道不可用 → ${actualPath}`);
  }
});

test('every rule kind lists only attempted resources and excludes skipped or unvisited targets', () => {
  for (const provider of ['claude', 'codex']) {
    for (const kind of ['model', 'mapping', 'composite', 'non_stream', 'intent', 'default']) {
      const a = candidate(provider, 1, 11, 101), b = candidate(provider, 2, 21, 201);
      const request = { provider, model: 'test-model', routeSnapshot: trace([
        { candidate: b, action: 'skipped', reason: 'candidate_busy' },
        { candidate: a, action: 'attempt' }, { candidate: a, action: 'success' },
      ], { decision: { kind, targets: [{ channel_id: 2, group_id: 21, upstream_model: 'unused-model' }, { profile_id: 99 }] } }) };
      request.dispatchTrace = readRequestDispatchTrace(request, resources);
      assert.equal(describeRequestRoutePath(request), provider === 'codex' ? 'test-model\n渠道 A / 分组 A' : 'test-model\nClaude 渠道 / Claude 分组');
    }
  }
});

test('actual model changes across retries stay paired with the resource that served each model', () => {
  const a = { ...candidate('codex', 1, 11, 101), model: 'mapped-model' };
  const b = { ...candidate('codex', 2, 21, 201), model: 'client-model' };
  const request = { provider: 'codex', model: 'client-model', routeSnapshot: trace([
    { candidate: a, action: 'attempt' }, { candidate: a, action: 'failed' },
    { candidate: a, action: 'attempt' }, { candidate: a, action: 'failed' },
    { candidate: b, action: 'attempt' }, { candidate: b, action: 'success' },
  ], { requested_model: 'client-model', decision: { kind: 'mapping' } }) };
  request.dispatchTrace = readRequestDispatchTrace(request, resources);
  assert.equal(describeRequestRoutePath(request), 'client-model → mapped-model\n渠道 A / 分组 A\n\nclient-model\n渠道 B / 分组 B');
  assert.equal(request.dispatchTrace.attemptCount, 3);
});

test('zero-send requests ignore stale resource and model rewrite fields', () => {
  for (const stop_reason of ['', 'route_unavailable']) {
    const request = { model: 'mapped-model', originalModelName: 'client-model', modelRewriteTarget: 'mapped-model',
      channel: '旧渠道', endpoint: '旧端点', endpointChain: ['旧渠道::旧分组::旧端点'],
      routeSnapshot: trace([], { attempts: 0, stop_reason, decision: { kind: 'mapping', targets: [{ channel_id: 1, group_id: 11, upstream_model: 'mapped-model' }] } }),
    };
    assert.equal(describeRequestRoutePath(request), '');
  }
});

test('historical attempts without candidate events can use the saved endpoint chain', () => {
  const request = { provider: 'codex', model: 'M', endpointChain: ['历史渠道::历史分组::端点'],
    routeSnapshot: trace([], { attempts: 1, decision: { kind: 'model', targets: [{ channel_id: 2, group_id: 21 }] } }),
  };
  assert.equal(describeRequestRoutePath(request), 'M\n历史渠道 / 历史分组');
});

test('dispatch trace distinguishes actual attempts, repeated retries, skipped resources and terminal events', () => {
  const a = candidate('codex', 1, 11, 101), b = candidate('codex', 2, 21, 201);
  const result = readRequestDispatchTrace({ provider: 'codex', route_snapshot: JSON.stringify(trace([
    { candidate: b, action: 'skipped', reason: 'candidate_busy' },
    { candidate: a, action: 'attempt' }, { candidate: a, action: 'failed', reason: 'upstream_error' },
    { candidate: a, action: 'attempt' }, { candidate: a, action: 'failed', reason: 'upstream_error' },
    { candidate: b, action: 'attempt' }, { candidate: b, action: 'response_started' }, { candidate: b, action: 'success' },
  ], { attempts: 3, successful: true, finished_at: '2026-10-28T17:00:00+08:00' })) }, resources);
  assert.equal(result.profileName, '请求当时的方案');
  assert.equal(result.attemptCount, 3);
  assert.deepEqual(result.attempts.map(item => [item.channel, item.credentialGroup, item.endpoint, item.action]), [
    ['渠道 A', '分组 A', '主端点', 'failed'], ['渠道 A', '分组 A', '主端点', 'failed'], ['渠道 B', '分组 B', '备用端点', 'success'],
  ]);
  assert.equal(result.skipped.length, 1);
});

test('Claude resource keys and managed credentials resolve without leaking account identity', () => {
  const c = { ...candidate('claude', 1, 11, 'Claude 渠道::g11::主端点'), credential: 'account:PRIVATE_ACCOUNT_HASH' };
  const result = readRequestDispatchTrace({ provider: 'claude', routeSnapshot: trace([{ candidate: c, action: 'attempt' }]) }, resources);
  assert.equal(result.attempts[0].channel, 'Claude 渠道');
  assert.equal(result.attempts[0].credentialGroup, 'Claude 分组');
  assert.equal(result.attempts[0].endpoint, '主端点');
  assert.ok(!JSON.stringify(result).includes('PRIVATE_ACCOUNT_HASH'));
});

test('historical path names and hop diagnostics survive renamed or deleted resources', () => {
  const c = candidate('codex', 1, 11, 101);
  const request = { provider: 'codex', channel: '历史渠道', endpoint: '历史端点', endpoint_chain: [{ channel: '历史渠道', credential_group: '历史分组', endpoint: '历史端点', duration_ms: 12, status_code: 429 }], route_snapshot: trace([{ candidate: c, action: 'attempt' }, { candidate: c, action: 'failed' }], { attempts: 1 }) };
  const result = readRequestDispatchTrace(request, resources);
  assert.equal(result.attempts[0].channel, '历史渠道');
  assert.equal(result.attempts[0].endpoint, '历史端点');
  assert.equal(result.attempts[0].credentialGroup, '历史分组');
  assert.equal(result.attempts[0].durationMs, 12);
  assert.equal(result.attempts[0].statusCode, 429);
  const deleted = readRequestDispatchTrace({ routeSnapshot: trace([{ candidate: c, action: 'attempt' }]) });
  assert.equal(deleted.attempts[0].channel, '渠道 #1');
  assert.equal(deleted.attempts[0].endpoint, '端点 #101');
});

test('admission rejection has zero upstream attempts and old records never inherit the current profile', () => {
  const c = candidate('codex', 1, 11, 101);
  const result = readRequestDispatchTrace({ routeSnapshot: trace([{ candidate: c, action: 'skipped', reason: 'manual_balance_exhausted' }], { attempts: 0, stop_reason: 'route_unavailable' }) }, resources);
  assert.equal(result.attemptCount, 0);
  assert.equal(result.attempts.length, 0);
  assert.equal(result.skipped.length, 1);
  for (const value of ['', 'broken-json', '[]', null, { kind: 'model_rule', name: '旧规则' }]) {
    assert.equal(readRequestDispatchTrace({ route_snapshot: value }, resources), null);
  }
});

test('historical group identity survives moving an endpoint to another credential group', () => {
  const c = candidate('codex', 1, 11, 101);
  const moved = [{ ...resources[0], credentialGroups: [{ id: 11, name: '历史分组' }, { id: 12, name: '新分组' }], endpoints: [{ id: 101, name: '主端点', credentialGroupId: 12 }] }];
  const result = readRequestDispatchTrace({ provider: 'codex', route_snapshot: trace([{ candidate: c, action: 'attempt' }]) }, moved);
  assert.equal(result.attempts[0].credentialGroup, '历史分组');
});

test('grouped historical hops retain names without assigning aggregate diagnostics to each retry', () => {
  const a = candidate('codex', 1, 11, 101), b = candidate('codex', 2, 21, 201);
  const result = readRequestDispatchTrace({ provider: 'codex', routeSnapshot: trace([
    { candidate: a, action: 'attempt' }, { candidate: a, action: 'attempt' }, { candidate: a, action: 'failed' },
    { candidate: b, action: 'attempt' }, { candidate: b, action: 'success' },
  ]), endpoint_chain: [
    { channel: '渠道 A', credential_group: '分组 A', endpoint: '主端点', attempts: 2, duration_ms: 200, status_code: 503 },
    { channel: '渠道 B', credential_group: '分组 B', endpoint: '备用端点', attempts: 1, duration_ms: 300 },
  ] }, resources);
  assert.deepEqual(result.attempts.map(item => item.endpoint), ['主端点', '主端点', '备用端点']);
  assert.ok(result.attempts.slice(0, 2).every(item => item.durationMs == null && !item.statusCode));
  assert.equal(result.attempts[2].durationMs, 300);
});

test('advanced diagnostics retain historical identities, zero state versions and every candidate event across retries', () => {
  const c = { ...candidate('codex', 1, 11, 101), state_version: 0 };
  const next = { ...c, state_version: 9 };
  const result = readRequestDispatchTrace({ provider: 'codex', routeSnapshot: trace([
    { candidate: c, action: 'attempt' }, { candidate: c, action: 'failed', reason: 'network_error', failure_scope: 'endpoint' },
    { candidate: c, action: 'attempt' }, { candidate: c, action: 'success' },
    { candidate: next, action: 'skipped', reason: 'candidate_busy' },
    { candidate: next, action: 'failed', reason: 'route_unavailable' },
  ], { request_id: 'historical-request-id' }) }, [{ ...resources[0], id: 99 }]);
  assert.equal(result.requestId, 'historical-request-id');
  assert.equal(result.attempts.length, 2);
  assert.equal(result.diagnostics.length, 2);
  const [first, second] = result.diagnostics;
  assert.deepEqual([first.channelId, first.groupId, first.endpointId, first.credentialLabel, first.stateVersion], ['1', '11', '101', '1/11', '0']);
  assert.deepEqual(first.events.map(event => event.action), ['attempt', 'failed', 'attempt', 'success']);
  assert.deepEqual(first.events[1], { action: 'failed', reason: 'network_error', scope: 'endpoint' });
  assert.equal(second.stateVersion, '9');
  assert.deepEqual(second.events.map(event => event.action), ['skipped', 'failed']);
});

test('advanced diagnostics hide managed account hashes and unexpected credential values and distinguish missing versions', () => {
  for (const credential of ['account:PRIVATE_ACCOUNT_HASH', 'unexpected-secret-placeholder']) {
    const c = { ...candidate('claude', 1, 11, 'Claude 渠道::g11::主端点'), credential };
    const result = readRequestDispatchTrace({ provider: 'claude', routeSnapshot: trace([{ candidate: c, action: 'skipped', reason: 'candidate_busy' }]) }, resources);
    assert.equal(result.diagnostics[0].groupId, '11');
    assert.equal(result.diagnostics[0].stateVersion, null);
    assert.ok(result.diagnostics[0].credentialLabel.includes('隐藏'));
    assert.ok(!JSON.stringify(result).includes(credential));
  }
});
