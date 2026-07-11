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
});

test('request network bridge maps snake and camel case fields without losing zero retries', () => {
  const mapped = wailsApi.mapRequestNetworkFields({
    httpStatus: 204,
    retryCount: 0,
    method: 'DELETE',
    path: '/v1/responses/1',
    clientIP: '192.0.2.20:4321',
    userAgent: 'Codex Desktop/1.0',
  });

  assert.deepEqual(mapped, {
    status_code: 204,
    http_status_code: 204,
    retry_count: 0,
    method: 'DELETE',
    path: '/v1/responses/1',
    client_ip: '192.0.2.20:4321',
    user_agent: 'Codex Desktop/1.0',
  });
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

test('Codex credential-group bridge preserves cache-write and long-context billing fields', () => {
  assert.equal(typeof wailsApi.mapCodexCredentialGroup, 'function');
  assert.equal(typeof wailsApi.buildCodexCredentialGroupPayload, 'function');

  const mapped = wailsApi.mapCodexCredentialGroup({
    cacheCreationCostMultiplier: 1.2,
    longContextCostMultiplier: 1.5,
    longContextInputCostMultiplier: 1.1,
    longContextOutputCostMultiplier: 1.2,
    longContextCacheCreationCostMultiplier: 1.3,
    longContextCacheReadCostMultiplier: 1.4,
    disableLongContextPricing: true
  });
  assert.equal(mapped.cacheCreationCostMultiplier, 1.2);
  assert.equal(mapped.longContextCostMultiplier, 1.5);
  assert.equal(mapped.longContextInputCostMultiplier, 1.1);
  assert.equal(mapped.longContextOutputCostMultiplier, 1.2);
  assert.equal(mapped.longContextCacheCreationCostMultiplier, 1.3);
  assert.equal(mapped.longContextCacheReadCostMultiplier, 1.4);
  assert.equal(mapped.disableLongContextPricing, true);

  const payload = wailsApi.buildCodexCredentialGroupPayload({
    cacheCreationCostMultiplier: '6/5',
    longContextCostMultiplier: '3/2',
    longContextInputCostMultiplier: '11/10',
    longContextOutputCostMultiplier: '6/5',
    longContextCacheCreationCostMultiplier: '13/10',
    longContextCacheReadCostMultiplier: '7/5',
    disableLongContextPricing: true
  });
  assert.equal(payload.cacheCreationCostMultiplier, 1.2);
  assert.equal(payload.longContextCostMultiplier, 1.5);
  assert.equal(payload.longContextInputCostMultiplier, 1.1);
  assert.equal(payload.longContextOutputCostMultiplier, 1.2);
  assert.equal(payload.longContextCacheCreationCostMultiplier, 1.3);
  assert.equal(payload.longContextCacheReadCostMultiplier, 1.4);
  assert.equal(payload.disableLongContextPricing, true);
});

test('Claude credential-group bridge preserves long-context billing fields', () => {
  const mapped = wailsApi.mapClaudeCredentialGroup({
    long_context_cost_multiplier: 1.25,
    long_context_input_cost_multiplier: 1.1,
    long_context_output_cost_multiplier: 1.2,
    long_context_cache_creation_cost_multiplier: 1.3,
    long_context_cache_read_cost_multiplier: 1.4,
    disable_long_context_pricing: true
  });
  assert.equal(mapped.longContextCostMultiplier, 1.25);
  assert.equal(mapped.longContextInputCostMultiplier, 1.1);
  assert.equal(mapped.longContextOutputCostMultiplier, 1.2);
  assert.equal(mapped.longContextCacheCreationCostMultiplier, 1.3);
  assert.equal(mapped.longContextCacheReadCostMultiplier, 1.4);
  assert.equal(mapped.disableLongContextPricing, true);

  const payload = wailsApi.buildClaudeCredentialGroupPayload({
    longContextCostMultiplier: '5/4',
    longContextInputCostMultiplier: '11/10',
    longContextOutputCostMultiplier: '6/5',
    longContextCacheCreationCostMultiplier: '13/10',
    longContextCacheReadCostMultiplier: '7/5',
    disableLongContextPricing: true
  });
  assert.equal(payload.long_context_cost_multiplier, 1.25);
  assert.equal(payload.long_context_input_cost_multiplier, 1.1);
  assert.equal(payload.long_context_output_cost_multiplier, 1.2);
  assert.equal(payload.long_context_cache_creation_cost_multiplier, 1.3);
  assert.equal(payload.long_context_cache_read_cost_multiplier, 1.4);
  assert.equal(payload.disable_long_context_pricing, true);
});
