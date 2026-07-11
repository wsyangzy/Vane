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
