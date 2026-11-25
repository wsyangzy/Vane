import test from 'node:test';
import assert from 'node:assert/strict';
import { buildEndpointProbeInput, getDetectedPathStrategy, previewEndpointPath } from './pathPolicy.js';
import { buildCodexEndpointMutationPayload } from '../shared/endpoints/endpointActions.js';

test('unsaved setup draft supplies its credential instead of resolving temporary negative ids', () => {
  const input = buildEndpointProbeInput({ channel: { id: -1 }, credentialGroup: { id: -1, apiKey: 'draft-test-key' }, baseURL: 'https://api.invalid', pathStrategy: 'v1', model: 'test', verifyResponse: false });
  assert.equal(input.apiKey, 'draft-test-key');
  assert.equal(input.channelId, 0);
  assert.equal(input.credentialGroupId, 0);
  const saved = buildEndpointProbeInput({ channel: { id: 4 }, credentialGroup: { id: 8, apiKeyMasked: 'masked' }, endpoint: { id: 12 } });
  assert.equal(saved.apiKey, undefined);
  assert.equal(saved.channelId, 4);
  assert.equal(saved.credentialGroupId, 8);
  assert.equal(saved.endpointId, 12);
});

test('endpoint path preview supports root, versioned and custom prefixes', () => {
  assert.equal(previewEndpointPath('https://api.invalid/', 'base'), 'https://api.invalid/responses');
  assert.equal(previewEndpointPath('https://api.invalid/custom', 'v1'), 'https://api.invalid/custom/v1/responses');
  assert.equal(previewEndpointPath('https://api.invalid/v1/', 'v1'), 'https://api.invalid/v1/responses');
});

test('unrelated endpoint flag mutations retain explicit path policy', () => {
  const payload = buildCodexEndpointMutationPayload({ name: 'test', baseURL: 'https://api.invalid', pathStrategy: 'base' }, { skipHealthCheck: true });
  assert.equal(payload.pathStrategy, 'base');
  assert.equal(payload.skipHealthCheck, true);
});

test('batch applies only paths confirmed by the model directory', () => {
  const base = { path_strategy: 'base', verified: true };
  const v1 = { path_strategy: 'v1', verified: true };
  const detect = (current, ...candidates) => getDetectedPathStrategy({ candidates }, current);
  assert.equal(detect('legacy', base), 'v1', 'equivalent URLs resolve to one probe and use an explicit policy');
  assert.equal(detect('base', base), 'base');
  assert.equal(detect('legacy', base, { http_status: 404 }), 'base');
  assert.equal(detect('legacy', { http_status: 308 }, v1), 'v1');
  assert.equal(detect('legacy', { http_status: 200, result: 'invalid_response' }, v1), 'v1');
  assert.equal(detect('legacy', base, v1), '');
  for (const http_status of [401, 403, 429, 500, 0]) {
    assert.equal(detect('legacy', { http_status }, v1), '');
  }
  assert.equal(detect('legacy', { http_status: 403 }), '');
});
