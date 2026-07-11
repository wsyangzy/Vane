import test from 'node:test';
import assert from 'node:assert/strict';

import { fetchRequests } from './api.js';

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
        retry_count: 2,
        method: 'PATCH',
        path: '/v1/responses',
        client_ip: '192.0.2.10:1234',
        user_agent: 'Codex CLI/1.0',
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
  } finally {
    globalThis.fetch = originalFetch;
  }
});
