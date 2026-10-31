import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { buildDispatchInput, buildIntentClassificationInput, buildRoutingProfilePayload, buildManualBalanceUpdate, listManualBalances, mapManualBalance, mapDispatchPreview, mapDispatchTrace, mapDispatchWorkspace, mapQuotaSnapshot, mapRoutingConfigurationStatus, mapRoutingProfile } from './dispatch.js';

test('dispatch reports the desktop requirement when no Wails runtime is present', async () => {
  await assert.rejects(listManualBalances('codex'), /需要在 Vane 桌面应用中使用/);
});

test('manual balance bridge preserves money strings and explicitly maps update fields', () => {
  const balance = mapManualBalance({ provider: 'codex', channel_id: 1, group_id: 2, enabled: true, binding_changed: false, balance_usd: '2000000000.000000001', spent_usd: '0.000000001', stop_when_exhausted: true, revision: 4 });
  assert.equal(balance.balanceUsd, '2000000000.000000001');
  assert.equal(balance.spentUsd, '0.000000001');
  assert.deepEqual(buildManualBalanceUpdate({ ...balance, action: 'credit', amountUsd: ' 0.000000001 ', operationId: 'stable-operation', accountIdHash: 'never-forward' }), { provider: 'codex', channel_id: 1, group_id: 2, action: 'credit', amount_usd: '0.000000001', stop_when_exhausted: true, revision: 4, operation_id: 'stable-operation' });
  assert.equal(mapDispatchWorkspace({ balances: [{ balance_usd: '0' }] }).balances[0].balanceUsd, '0');
});

test('routing profile contract preserves nullable fallbacks, strings and duration units', () => {
  const dto = { id: 12, provider: 'codex', name: '编程方案', enabled: true, is_default: true, candidate_mode: 'unified_candidates', strategy: 'weighted', affinity_mode: 'adaptive', max_attempts: 7, max_duration: 31000000000, wait_for_recovery: true, revision: 4,
    members: [{ profile_id: 12, channel_id: 3, group_id: 8, position: 0, weight: 5, enabled: false, upstream_model: 'gpt-mapped' }],
    rules: [{ id: 1, profile_id: 12, enabled: true, kind: 'model', name: '模型', priority: 10, match_model: 'gpt-*', require_stream: false, intent_id: '', mode: 'prefer', targets: [{ profile_id: 7, channel_id: 0, group_id: 0, upstream_model: '' }], fallback_to_default: false }, { id: 2, profile_id: 12, enabled: true, kind: 'non_stream', name: '', priority: 20, match_model: '', intent_id: '', mode: 'fallback', targets: [], require_stream: null, fallback_to_default: null }],
    intents: [{ id: 'coding-v2', profile_id: 12, name: '编程', description: '代码修改', enabled: true, priority: 3, targets: [{ profile_id: 0, channel_id: 3, group_id: 8, upstream_model: 'gpt-mapped' }], mode: 'prefer', fallback_intent: '', fallback_to_default: true }],
    classifier: { enabled: true, provider: 'claude', channel_id: 9, group_id: 10, model: 'claude-fast', timeout: 8000000000, confidence: 0.75, failure_mode: 'previous' }
  };
  const model = mapRoutingProfile(dto);
  assert.equal(model.maxDurationMs, 31000); assert.equal(model.classifier.timeoutMs, 8000);
  assert.equal(model.rules[0].requireStream, false); assert.equal(model.rules[1].requireStream, null);
  assert.equal(model.intents[0].id, 'coding-v2'); assert.equal(model.revision, 4);
  model.intents[0].rowKey = 'ui-only-row-key'; model.apiKey = 'never-forward-this';
  const payload = buildRoutingProfilePayload(model);
  assert.deepEqual(payload, dto);
  assert.equal(JSON.stringify(payload).includes('ui-only-row-key'), false);
  assert.equal(JSON.stringify(payload).includes('never-forward-this'), false);
});

test('workspace and quota mapping distinguish unknown from numeric zero across model scopes', () => {
  const state = mapDispatchWorkspace({ profiles: [], channels: [{ id: 1, name: '渠道' }], accounts: [{ channel_id: 1, group_id: 2, group_name: '订阅', enabled: false, managed: true, auth_mode: 'subscription' }], quota: [{ provider: 'codex', channel_id: 1, group_id: 2, model_scope: 'gpt-a', window_name: '5h', used_percent: 0, confidence: 'exact' }, { provider: 'codex', channel_id: 1, group_id: 2, model_scope: 'gpt-b', window_name: '5h', confidence: 'unknown' }], runtime_mode: 'unified' });
  assert.equal(Object.hasOwn(state, 'runtimeMode'), false); assert.equal(state.accounts[0].enabled, false);
  assert.deepEqual(state.quotas.map((quota) => [quota.modelScope, quota.usedPercent]), [['gpt-a', 0], ['gpt-b', null]]);
  assert.equal(mapQuotaSnapshot({ remaining: 0, limit: 0 }).remaining, 0);
});

test('preview and completed trace map distinct candidate shapes without treating response start as success', () => {
  const preview = mapDispatchPreview({ profile_id: 1, decision: { kind: 'model', rule_id: 3, fallback_to_default: false, reused_turn: true }, candidates: [{ candidate: { provider: 'codex', channel_id: 2, group_id: 3, endpoint_id: 'ep-1', upstream_model: 'gpt-a', health: { cooling_down: true, reason: 'rate_limit' } }, eligible: false, reason: 'cooling_down', state_compatibility: 'incompatible', quota_known: true, used_percent: 0, load: { concurrent: 2, max_concurrent: 2 } }] });
  assert.equal(preview.decision.reusedTurn, true); assert.equal(preview.decision.fallbackToDefault, false);
  assert.equal(preview.candidates[0].candidate.health.coolingDown, true); assert.equal(preview.candidates[0].usedPercent, 0);
  assert.equal(preview.candidates[0].stateCompatibility, 'incompatible');
  const trace = mapDispatchTrace({ profile_id: 1, attempts: 2, response_started: true, successful: false, stop_reason: 'stream_incomplete', candidates: [{ candidate: { provider: 'codex', channel: '2', credential: '2/3', endpoint: 'ep-1', model: 'gpt-a', state_version: 4 }, action: 'failed', reason: 'network_error', failure_scope: 'endpoint' }], intent: { intent_id: 'coding', cost: 0.002, input_tokens: 100, output_tokens: 10 } });
  assert.equal(trace.attempts, 2); assert.equal(trace.responseStarted, true); assert.equal(trace.successful, false);
  assert.equal(trace.candidates[0].failureScope, 'endpoint'); assert.equal(trace.candidates[0].candidate.credential, '2/3');
  assert.equal(trace.intent.cost, 0.002); assert.equal(trace.intent.inputTokens, 100);
});

test('dispatch input explicitly allowlists fields and configuration checks preserve failed conditions', () => {
  const input = buildDispatchInput({ provider: 'claude', profileId: 3, model: ' claude-a ', streaming: false, conversationId: 'c', branchId: 'b', turnId: 't', newTurn: true, text: 'private text', apiKey: 'secret' });
  assert.deepEqual(input, { provider: 'claude', profile_id: 3, model: 'claude-a', streaming: false, conversation_id: 'c', branch_id: 'b', turn_id: 't', new_turn: true });
  const status = mapRoutingConfigurationStatus({ ready: false, checks: [{ code: 'default_profile', passed: false, message: '请先建立默认方案' }] });
  assert.equal(status.ready, false); assert.equal(status.checks[0].passed, false);
});

test('dispatch bridge resolves actual generated methods and DispatchPlan fields', () => {
  const bridge = readFileSync(new URL('./dispatch.js', import.meta.url), 'utf8');
  const service = readFileSync(new URL('../../bindings/github.com/wsyangzy/Vane/internal/desktop/api/routingservice.js', import.meta.url), 'utf8');
  for (const [, method] of bridge.matchAll(/invoke\('([A-Za-z]+)'/g)) assert.ok(service.includes(`export function ${method}(`), `${method} missing from actual generated routing service`);
  const appModels = readFileSync(new URL('../../bindings/github.com/wsyangzy/Vane/internal/app/models.js', import.meta.url), 'utf8');
  const routingModels = readFileSync(new URL('../../bindings/github.com/wsyangzy/Vane/internal/routing/models.js', import.meta.url), 'utf8');
  for (const field of ['current_user_message', 'previous_user_message', 'previous_assistant_text', 'quota', 'ready', 'checks']) assert.ok(appModels.includes(`this["${field}"]`), `app DTO missing ${field}`);
  for (const field of ['profile', 'evaluations', 'stages', 'eligible_candidates', 'response_started', 'successful', 'max_duration', 'fallback_to_default']) assert.ok(routingModels.includes(`this["${field}"]`) || routingModels.includes(`"${field}" in`), `routing DTO missing ${field}`);
  const plan = mapDispatchPreview({ profile: { id: 8, name: '实际方案', strategy: 'pace', affinity_mode: 'turn' }, evaluations: [{ candidate: { channel_id: 2, group_id: 3 }, eligible: true }], stages: [{ index: 0, kind: 'rule', evaluations: [], eligible_candidates: [] }], error: 'budget_exhausted' });
  assert.equal(plan.profileId, 8); assert.equal(plan.strategy, 'pace'); assert.equal(plan.candidates.length, 1); assert.equal(plan.stages[0].kind, 'rule'); assert.equal(plan.stopReason, 'budget_exhausted');
  const classification = buildIntentClassificationInput({ provider: 'codex', text: 'current', previousUserMessage: 'previous', previousAssistantText: 'summary', apiKey: 'never-include' });
  assert.equal(classification.current_user_message, 'current'); assert.equal(classification.previous_assistant_text, 'summary'); assert.equal(Object.hasOwn(classification, 'text'), false); assert.equal(Object.hasOwn(classification, 'apiKey'), false);
});
