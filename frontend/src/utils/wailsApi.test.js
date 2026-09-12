import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

import * as wailsApi from './wailsApi.js';

test('generated RequestRecord model includes request network fields', () => {
  const source = readFileSync(new URL('../wailsjs/go/models.ts', import.meta.url), 'utf8');
  const requestRecord = source.match(/export class RequestRecord \{([\s\S]*?)static createFrom/)?.[1] || '';

  for (const field of ['client_ip', 'user_agent', 'method', 'path']) {
    assert.match(requestRecord, new RegExp(`\\b${field}: string;`));
  }
  assert.match(requestRecord, /last_failure_reason\?: string;/);
  assert.match(requestRecord, /http_status_is_upstream: boolean;/);
  const bridge = readFileSync(new URL('./wailsApi.js', import.meta.url), 'utf8');
  assert.match(bridge, /last_failure_reason: r\.last_failure_reason/);
});

test('generated App binding exposes the data directory action', () => {
  const source = readFileSync(new URL('../wailsjs/go/main/App.js', import.meta.url), 'utf8');
  assert.match(source, /export function OpenDataDir\(\)/);
  assert.match(source, /Call\.ByName\("main\.App\.OpenDataDir"\)/);
});

test('generated App binding exposes credential-group daily usage', () => {
  const source = readFileSync(new URL('../wailsjs/go/main/App.js', import.meta.url), 'utf8');
  assert.match(source, /export function GetCredentialGroupDailyUsage\(arg1, arg2, arg3, arg4\)/);
  assert.match(source, /Call\.ByName\("main\.App\.GetCredentialGroupDailyUsage"/);
});

test('generated Codex credential-group models keep combination billing rules', () => {
  const source = readFileSync(new URL('../wailsjs/go/models.ts', import.meta.url), 'utf8');
  for (const model of ['CodexCredentialGroupInfo', 'CreateCodexCredentialGroupInput', 'UpdateCodexCredentialGroupInput']) {
    const body = source.match(new RegExp(`export class ${model} \\{([\\s\\S]*?)static createFrom`))?.[1] || '';
    assert.match(body, /billingRules: any\[\];/);
    // 档位倍率已由 model_pricing 的 service_tier 维度取代，绑定里不应再出现
    assert.doesNotMatch(body, /serviceTierMultipliers/);
    assert.doesNotMatch(body, /disableServiceTierPricing/);
  }
});

test('request network bridge maps snake and camel case fields without losing zero retries', () => {
  const mapped = wailsApi.mapRequestNetworkFields({
    httpStatus: 204,
    httpStatusIsUpstream: true,
    retryCount: 0,
    method: 'DELETE',
    path: '/v1/responses/1',
    clientIP: '192.0.2.20:4321',
    userAgent: 'Codex Desktop/1.0',
  });

  assert.deepEqual(mapped, {
    status_code: 204,
    http_status_code: 204,
    http_status_is_upstream: true,
    httpStatusIsUpstream: true,
    retry_count: 0,
    method: 'DELETE',
    path: '/v1/responses/1',
    client_ip: '192.0.2.20:4321',
    user_agent: 'Codex Desktop/1.0',
  });
});

test('request network bridge hides explicitly Relay-generated status codes', () => {
  const mapped = wailsApi.mapRequestNetworkFields({
    http_status: 502,
    http_status_is_upstream: false,
  });

  assert.equal(mapped.status_code, 0);
  assert.equal(mapped.http_status_code, 0);
  assert.equal(mapped.httpStatusIsUpstream, false);
});

test('request network bridge hides status codes without an origin flag', () => {
  const mapped = wailsApi.mapRequestNetworkFields({ http_status: 503 });

  assert.equal(mapped.status_code, 0);
  assert.equal(mapped.http_status_code, 0);
  assert.equal(mapped.httpStatusIsUpstream, false);
});

test('parseEndpointPriority preserves zero as the highest priority', () => {
  assert.equal(wailsApi.parseEndpointPriority(0), 0);
  assert.equal(wailsApi.parseEndpointPriority('0'), 0);
});

test('parseEndpointPriority rejects invalid and negative values', () => {
  assert.equal(wailsApi.parseEndpointPriority('', 1), 1);
  assert.equal(wailsApi.parseEndpointPriority('2.5', 1), 1);
  assert.equal(wailsApi.parseEndpointPriority('-1', 1), 1);
});

test('model pricing bridge preserves long-context fields', () => {
  assert.equal(typeof wailsApi.mapModelPricingRecord, 'function');
  assert.equal(typeof wailsApi.buildModelPricingPayload, 'function');

  const mapped = wailsApi.mapModelPricingRecord({
    model_name: 'gpt-long',
    provider: 'codex',
    long_context_threshold: 272000,
    long_context_input_price: 7.5,
    long_context_cache_read_price: 0.75,
    long_context_cache_write_price: 9.375,
    long_context_output_price: 45
  });
  assert.deepEqual({
    threshold: mapped.longContextThreshold,
    input: mapped.longContextInputPrice,
    cacheRead: mapped.longContextCacheReadPrice,
    cacheWrite: mapped.longContextCacheWritePrice,
    output: mapped.longContextOutputPrice
  }, {
    threshold: 272000,
    input: 7.5,
    cacheRead: 0.75,
    cacheWrite: 9.375,
    output: 45
  });

  const payload = wailsApi.buildModelPricingPayload({
    modelName: 'gpt-long',
    provider: 'codex',
    longContextThreshold: '272000',
    longContextInputPrice: '7.5',
    longContextCacheReadPrice: '0.75',
    longContextCacheWritePrice: '9.375',
    longContextOutputPrice: '45'
  });
  assert.deepEqual({
    threshold: payload.long_context_threshold,
    input: payload.long_context_input_price,
    cacheRead: payload.long_context_cache_read_price,
    cacheWrite: payload.long_context_cache_write_price,
    output: payload.long_context_output_price
  }, {
    threshold: 272000,
    input: 7.5,
    cacheRead: 0.75,
    cacheWrite: 9.375,
    output: 45
  });
});

test('model pricing bridge carries provider and service tier end to end', () => {
  const mapped = wailsApi.mapModelPricingRecord({
    id: 42,
    model_name: 'gpt-5.6-sol',
    provider: 'codex',
    service_tier: 'flex'
  });
  assert.equal(mapped.id, 42);
  assert.equal(mapped.provider, 'codex');
  assert.equal(mapped.serviceTier, 'flex');
  // 标准档主行的档位是空串，不能被 undefined 污染成「未知档」
  assert.equal(wailsApi.mapModelPricingRecord({ model_name: 'm' }).serviceTier, '');

  const tierRow = wailsApi.buildModelPricingPayload({
    id: 42,
    modelName: 'gpt-5.6-sol',
    provider: 'codex',
    serviceTier: 'flex'
  });
  assert.equal(tierRow.id, 42);
  assert.equal(tierRow.provider, 'codex');
  assert.equal(tierRow.service_tier, 'flex');
  // 新建时无 id，后端按 (provider, model_name, service_tier) 三元组定位
  const created = wailsApi.buildModelPricingPayload({ modelName: 'm' });
  assert.equal(created.id, 0);
  assert.equal(created.service_tier, '');
});

test('generated model pricing bindings expose provider and service tier', () => {
  const models = readFileSync(new URL('../wailsjs/go/models.ts', import.meta.url), 'utf8');
  for (const model of ['ModelPricingInfo', 'CreateModelPricingInput']) {
    const body = models.match(new RegExp(`export class ${model} \\{([\\s\\S]*?)static createFrom`))?.[1] || '';
    // 缺任一维度，同名模型就会跨 provider 串味或档位行无法定位
    assert.match(body, /provider: string;/);
    assert.match(body, /service_tier: string;/);
  }
  assert.match(models.match(/export class CreateModelPricingInput \{([\s\S]*?)static createFrom/)?.[1] || '', /id: number;/);

  const app = readFileSync(new URL('../wailsjs/go/main/App.js', import.meta.url), 'utf8');
  // 删除必须带 provider，否则是跨 provider 误删
  assert.match(app, /export function DeleteModelPricing\(arg1, arg2\)/);
  assert.match(app, /export function DeleteModelPricingTier\(arg1, arg2, arg3\)/);
});

test('Codex credential-group bridge preserves cache-write and long-context billing fields', () => {
  assert.equal(typeof wailsApi.mapCodexCredentialGroup, 'function');
  assert.equal(typeof wailsApi.buildCodexCredentialGroupPayload, 'function');

  const mapped = wailsApi.mapCodexCredentialGroup({
	 daily_cost_limit_usd: 12.5,
    cacheCreationCostMultiplier: 1.2,
    longContextCostMultiplier: 1.5,
    longContextInputCostMultiplier: 1.1,
    longContextOutputCostMultiplier: 1.2,
    longContextCacheCreationCostMultiplier: 1.3,
    longContextCacheReadCostMultiplier: 1.4,
    disableLongContextPricing: true
  });
  assert.equal(mapped.cacheCreationCostMultiplier, 1.2);
	assert.equal(mapped.dailyCostLimitUSD, 12.5);
  assert.equal(mapped.longContextCostMultiplier, 1.5);
  assert.equal(mapped.longContextInputCostMultiplier, 1.1);
  assert.equal(mapped.longContextOutputCostMultiplier, 1.2);
  assert.equal(mapped.longContextCacheCreationCostMultiplier, 1.3);
  assert.equal(mapped.longContextCacheReadCostMultiplier, 1.4);
  assert.equal(mapped.disableLongContextPricing, true);

  const payload = wailsApi.buildCodexCredentialGroupPayload({
	 dailyCostLimitUSD: '20.25',
    cacheCreationCostMultiplier: '6/5',
    longContextCostMultiplier: '3/2',
    longContextInputCostMultiplier: '11/10',
    longContextOutputCostMultiplier: '6/5',
    longContextCacheCreationCostMultiplier: '13/10',
    longContextCacheReadCostMultiplier: '7/5',
    disableLongContextPricing: true
  });
  assert.equal(payload.cacheCreationCostMultiplier, 1.2);
	assert.equal(payload.daily_cost_limit_usd, 20.25);
  assert.equal(payload.longContextCostMultiplier, 1.5);
  assert.equal(payload.longContextInputCostMultiplier, 1.1);
  assert.equal(payload.longContextOutputCostMultiplier, 1.2);
  assert.equal(payload.longContextCacheCreationCostMultiplier, 1.3);
  assert.equal(payload.longContextCacheReadCostMultiplier, 1.4);
  assert.equal(payload.disableLongContextPricing, true);
});

test('Codex credential-group bridge preserves sparse combination billing rules', () => {
  const mapped = wailsApi.mapCodexCredentialGroup({
    billing_rules: [{
      model: 'gpt-5.4',
      service_tier: 'priority',
      context: 'long',
      mode: 'fixed',
      input_price: 7.5,
      output_price: 45,
      cache_creation_price: 9.375,
      cache_read_price: 0.75
    }]
  });
  assert.equal(mapped.billingRules[0].serviceTier, 'fast');
  assert.equal(mapped.billingRules[0].cacheCreationPrice, 9.375);

  const payload = wailsApi.buildCodexCredentialGroupPayload({ billingRules: mapped.billingRules });
  assert.equal(payload.billingRules[0].service_tier, 'fast');
  assert.equal(payload.billingRules[0].mode, 'fixed');
  assert.equal(payload.billingRules[0].output_price, 45);
});

test('Claude credential-group bridge preserves long-context billing fields', () => {
  const mapped = wailsApi.mapClaudeCredentialGroup({
	 daily_cost_limit_usd: 8.75,
    long_context_cost_multiplier: 1.25,
    long_context_input_cost_multiplier: 1.1,
    long_context_output_cost_multiplier: 1.2,
    long_context_cache_creation_cost_multiplier: 1.3,
    long_context_cache_read_cost_multiplier: 1.4,
    disable_long_context_pricing: true
  });
  assert.equal(mapped.longContextCostMultiplier, 1.25);
	assert.equal(mapped.dailyCostLimitUSD, 8.75);
  assert.equal(mapped.longContextInputCostMultiplier, 1.1);
  assert.equal(mapped.longContextOutputCostMultiplier, 1.2);
  assert.equal(mapped.longContextCacheCreationCostMultiplier, 1.3);
  assert.equal(mapped.longContextCacheReadCostMultiplier, 1.4);
  assert.equal(mapped.disableLongContextPricing, true);

  const payload = wailsApi.buildClaudeCredentialGroupPayload({
	 dailyCostLimitUSD: '15.5',
    longContextCostMultiplier: '5/4',
    longContextInputCostMultiplier: '11/10',
    longContextOutputCostMultiplier: '6/5',
    longContextCacheCreationCostMultiplier: '13/10',
    longContextCacheReadCostMultiplier: '7/5',
    disableLongContextPricing: true
  });
  assert.equal(payload.long_context_cost_multiplier, 1.25);
	assert.equal(payload.daily_cost_limit_usd, 15.5);
  assert.equal(payload.long_context_input_cost_multiplier, 1.1);
  assert.equal(payload.long_context_output_cost_multiplier, 1.2);
  assert.equal(payload.long_context_cache_creation_cost_multiplier, 1.3);
  assert.equal(payload.long_context_cache_read_cost_multiplier, 1.4);
  assert.equal(payload.disable_long_context_pricing, true);
});

test('Claude credential-group bridge preserves model policy fields', () => {
  const mapped = wailsApi.mapClaudeCredentialGroup({
    model_policy: 'override',
    model_mapping: { 'claude-small': 'claude-haiku' },
    supported_models: ['claude-haiku']
  });
  assert.equal(mapped.modelPolicy, 'override');
  assert.deepEqual(mapped.modelMapping, { 'claude-small': 'claude-haiku' });
  assert.deepEqual(mapped.supportedModels, ['claude-haiku']);

  const payload = wailsApi.buildClaudeCredentialGroupPayload({
    modelPolicy: 'passthrough',
    modelMapping: { 'claude-small': 'claude-haiku' },
    supportedModelsText: 'claude-opus, claude-opus'
  });
  assert.equal(payload.model_policy, 'passthrough');
  assert.deepEqual(payload.model_mapping, { 'claude-small': 'claude-haiku' });
  assert.deepEqual(payload.supported_models, ['claude-opus', 'claude-opus']);
});
