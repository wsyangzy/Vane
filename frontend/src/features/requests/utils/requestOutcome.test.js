import test from 'node:test';
import assert from 'node:assert/strict';
import { getRequestOutcome, getRequestDisplayStatus } from './requestOutcome.js';

test('historical HTTP 200 rows use the finished dispatch outcome instead of false completion', () => {
  for (const provider of ['claude', 'codex']) {
    const request = { provider, status: 'completed', stream_outcome: 'completed', route_snapshot: JSON.stringify({
      profile_id: 1, decision: { kind: 'default' }, attempts: 2,
      finished_at: '2026-11-02T11:05:03+08:00', successful: false, response_started: true,
      stop_reason: 'incomplete_response', candidates: [],
    }) };
    assert.equal(getRequestDisplayStatus(request), 'incomplete');
    assert.equal(getRequestOutcome(request), 'incomplete');
    assert.equal(request.status, 'completed');
    assert.equal(request.stream_outcome, 'completed');
    assert.equal(getRequestDisplayStatus({ ...request, status: 'processing' }), 'processing');
  }
});

test('completed streams, cancellation and final failures retain distinct meanings', () => {
  const request = { status: 'completed', streamOutcome: 'completed', dispatchTrace: { finished: true, successful: false, attempts: [], stopReason: 'upstream_error' } };
  assert.equal(getRequestDisplayStatus(request), 'failed');
  assert.equal(getRequestDisplayStatus({ ...request, dispatchTrace: { ...request.dispatchTrace, stopReason: 'client_cancelled' } }), 'cancelled');
  assert.equal(getRequestDisplayStatus({ ...request, dispatchTrace: { ...request.dispatchTrace, successful: true } }), 'completed');
  assert.equal(getRequestDisplayStatus({ status: 'completed', stream_outcome: 'incomplete' }), 'incomplete');
  assert.equal(getRequestDisplayStatus({ status: 'completed', failure_reason: 'stream_truncated' }), 'incomplete');
  assert.equal(getRequestDisplayStatus({ status: 'completed' }), 'completed');
  assert.equal(getRequestDisplayStatus({ status: 'failed' }), 'failed');
  assert.equal(getRequestOutcome({ status: 'cancelled', stream_outcome: 'canceled' }), 'canceled');
});
