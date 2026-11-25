import test from 'node:test';
import assert from 'node:assert/strict';
import { getRequestCompactionEvidence } from './compactionSupport.js';

const request = {
  provider: 'codex', path: '/v1/responses', channel: 'example', endpoint: 'primary', model: 'model-a',
  status: 'completed', isStreaming: true, streamOutcome: 'completed', httpStatusCode: 200, httpStatusIsUpstream: true,
  contextSummary: { protocol: { compaction_items: 1 } },
};

test('only complete upstream success carrying native state verifies compaction', () => {
  const result = getRequestCompactionEvidence(request);
  assert.equal(result.state, 'verified');
  assert.equal(result.compactionItems, 1);
  assert.equal(result.endpoint, 'primary');
  for (const change of [
    { streamOutcome: 'incomplete' }, { streamOutcome: 'failed' }, { streamOutcome: 'canceled' },
    { status: 'streaming' }, { in_progress: true }, { httpStatusIsUpstream: false },
    { streamOutcome: '' }, { contextSummary: { protocol: { compaction_items: 0 } } },
    { dispatchTrace: { finished: true, successful: false, attempts: [], stopReason: 'incomplete_response' } },
  ]) assert.equal(getRequestCompactionEvidence({ ...request, ...change }).state, 'unknown');
  assert.equal(getRequestCompactionEvidence({ ...request, streamOutcome: '', isStreaming: false }).state, 'verified');
});

test('upstream rejection records the result without asserting unsupported compaction', () => {
  const result = getRequestCompactionEvidence({ ...request, status: 'failed', httpStatusCode: 400, last_failure_reason: 'invalid tool type' });
  assert.equal(result.state, 'rejected');
  assert.match(result.description, /不能直接判定为不支持/);
  assert.equal(getRequestCompactionEvidence({ ...request, status: 'failed', httpStatusCode: 400, httpStatusIsUpstream: false }).state, 'unknown');
});

test('missing and malformed metadata remain unknown rather than implying zero native items', () => {
  for (const summary of [undefined, null, '{', 'null', '[]', {}, { protocol: {} }, { protocol: { compaction_items: -1 } }, { protocol: { compaction_items: '1' } }]) {
    const result = getRequestCompactionEvidence({ ...request, contextSummary: summary });
    assert.equal(result.state, 'unknown');
    assert.equal(result.compactionItems, null);
  }
  const result = getRequestCompactionEvidence({ ...request, contextSummary: { protocol: { compaction_items: 0 } } });
  assert.match(result.description, /未携带/);
});

test('raw rows and older native-item evidence work without inventing an exact count', () => {
  const raw = { provider: 'codex', status: 'completed', is_streaming: true, stream_outcome: 'completed',
    http_status_code: 200, http_status_is_upstream: true, context_summary: JSON.stringify({ protocol: { compaction_items: 2 } }) };
  assert.equal(getRequestCompactionEvidence(raw).state, 'verified');
  const old = getRequestCompactionEvidence({ ...raw, context_summary: JSON.stringify({ parts: { summary: { breakdown: [{ kind: 'compaction' }] } } }) });
  assert.equal(old.state, 'verified');
  assert.equal(old.compactionItems, null);
});

test('failover evidence is attributed to the responding endpoint and rewritten model', () => {
  const result = getRequestCompactionEvidence({ ...request, modelRewriteTarget: 'model-b', endpointChain: [
    { channel: 'first', endpoint: 'initial', status_code: 400, attempts: 1 },
    { channel: 'second', endpoint: 'final', status_code: 200, attempts: 1 },
    { channel: 'third', endpoint: 'skipped', status_code: 0, local: true, skip_reason: 'account_paused' },
  ] });
  assert.equal(result.state, 'verified');
  assert.equal(result.channel, 'second');
  assert.equal(result.endpoint, 'final');
  assert.equal(result.model, 'model-b');
});
