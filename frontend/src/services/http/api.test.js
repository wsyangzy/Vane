import test from 'node:test';
import assert from 'node:assert/strict';

import { fetchEndpointCostsData, fetchRequests, fetchTokenUsageData } from './api.js';

test('fetchRequests keeps an unselected endpoint empty instead of fabricating unknown', async () => {
  const originalFetch = globalThis.fetch;
  globalThis.fetch = async () => ({
    ok: true,
    headers: { get: () => 'application/json' },
    json: async () => ({
      requests: [{
        request_id: 'req-no-route',
        provider: 'codex',
        endpoint: '',
        endpoint_chain: [],
      }],
      total: 1,
      page: 1,
      pageSize: 50,
    }),
  });

  try {
    const result = await fetchRequests({ limit: 50 });
    const request = result.requests[0];
    assert.equal(request.endpoint, '');
    assert.equal(request.endpointChainLength, 0);
    assert.equal(request.endpointChainHead, '');
    assert.equal(request.endpointChainTail, '');
    assert.equal(request.group, '');
  } finally {
    globalThis.fetch = originalFetch;
  }
});

test('fetchRequests normalizes request network fields in both naming styles', async () => {
  const originalFetch = globalThis.fetch;
  globalThis.fetch = async () => ({
    ok: true,
    headers: { get: () => 'application/json' },
    json: async () => ({
      requests: [{
        request_id: 'req-network',
        http_status_code: 201,
        http_status_is_upstream: true,
        retry_count: 2,
        method: 'PATCH',
        path: '/v1/responses',
        client_ip: '192.0.2.10:1234',
        user_agent: 'Codex CLI/1.0',
		billing_rule_snapshot: '{"context_mode":"long","pricing_model":"gpt-5.6-sol","rule_mode":"fixed","rule_key":"model=*;tier=fast;context=long"}',
      }],
      total: 1,
      page: 1,
      pageSize: 50,
    }),
  });

  try {
    const request = (await fetchRequests({ limit: 50 })).requests[0];
    assert.equal(request.httpStatusCode, 201);
    assert.equal(request.http_status_code, 201);
    assert.equal(request.retryCount, 2);
    assert.equal(request.retry_count, 2);
    assert.equal(request.clientIP, '192.0.2.10:1234');
    assert.equal(request.client_ip, '192.0.2.10:1234');
    assert.equal(request.userAgent, 'Codex CLI/1.0');
    assert.equal(request.user_agent, 'Codex CLI/1.0');
	assert.equal(request.billingRuleSnapshot.rule_mode, 'fixed');
	assert.equal(request.billingRuleSnapshot.context_mode, 'long');
  } finally {
    globalThis.fetch = originalFetch;
  }
});

test('fetchRequests hides explicitly Relay-generated status codes', async () => {
  const originalFetch = globalThis.fetch;
  globalThis.fetch = async () => ({
    ok: true,
    headers: { get: () => 'application/json' },
    json: async () => ({
      requests: [{
        request_id: 'req-relay-status',
        http_status_code: 502,
        http_status_is_upstream: false,
      }],
      total: 1,
      page: 1,
      pageSize: 50,
    }),
  });

  try {
    const request = (await fetchRequests({ limit: 50 })).requests[0];
    assert.equal(request.httpStatusCode, null);
    assert.equal(request.http_status_code, null);
    assert.equal(request.statusCode, null);
  } finally {
    globalThis.fetch = originalFetch;
  }
});

test('fetchRequests hides status codes when origin is not declared', async () => {
  const originalFetch = globalThis.fetch;
  globalThis.fetch = async () => ({
    ok: true,
    headers: { get: () => 'application/json' },
    json: async () => ({
      requests: [{ request_id: 'req-legacy-status', http_status_code: 503 }],
      total: 1,
      page: 1,
      pageSize: 50,
    }),
  });

  try {
    const request = (await fetchRequests({ limit: 50 })).requests[0];
    assert.equal(request.httpStatusCode, null);
    assert.equal(request.httpStatusIsUpstream, false);
  } finally {
    globalThis.fetch = originalFetch;
  }
});

test('usage chart helpers propagate history query failures', async () => {
  const originalFetch = globalThis.fetch;
  const originalConsoleError = console.error;
  globalThis.fetch = async () => {
    throw new Error('归档 2026-06 查询失败');
  };
  console.error = () => {};

  try {
    await assert.rejects(fetchTokenUsageData(), /归档 2026-06 查询失败/);
    await assert.rejects(fetchEndpointCostsData(), /归档 2026-06 查询失败/);
  } finally {
    globalThis.fetch = originalFetch;
    console.error = originalConsoleError;
  }
});
