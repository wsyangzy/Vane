import test from 'node:test';
import assert from 'node:assert/strict';

import {
  chainHasMultipleChannels,
  getEndpointChainItems,
  getRequestNetworkDisplayState,
  parseChainNode,
  parseEndpointChain,
  parseEndpointName,
  summarizeEndpointAttempts,
} from './endpointChain.js';
import * as endpointChainUtils from './endpointChain.js';

test('parseEndpointChain keeps structured JSON chain nodes', () => {
  const chain = parseEndpointChain(JSON.stringify([
    { hop: 1, channel: 'CCTQ', credential_group: 'default', endpoint: 'Ark API' },
    { hop: 2, channel: 'Claude', credential_group: 'paid', endpoint: 'AWSQ' },
  ]));

  assert.equal(chain.length, 2);
  assert.deepEqual(parseChainNode(chain[0]), {
    raw: 'CCTQ::default::Ark API',
    channel: 'CCTQ',
    credentialGroup: 'default',
    endpoint: 'Ark API',
    statusCode: 0,
    error: '',
    hop: 1,
  });
  assert.equal(chainHasMultipleChannels(chain), true);
});

test('parseChainNode keeps structured endpoint error details', () => {
  const node = parseChainNode({
    hop: 1,
    channel: 'CCTQ',
    credential_group: 'default',
    endpoint: 'Ark API',
    status_code: 403,
    error: '{"error":{"message":"quota exceeded"}}',
  });

  assert.equal(node.statusCode, 403);
  assert.equal(node.error, '{"error":{"message":"quota exceeded"}}');
});

test('parseEndpointChain supports legacy transfer path strings', () => {
  const chain = parseEndpointChain('Claude::default::Ark API -> Claude::backup::AWSQ');

  assert.equal(chain.length, 2);
  assert.deepEqual(parseChainNode(chain[1], 2), {
    raw: 'Claude::backup::AWSQ',
    channel: 'Claude',
    credentialGroup: 'backup',
    endpoint: 'AWSQ',
    hop: 2,
  });
  assert.equal(parseEndpointName(chain[1]), 'AWSQ');
});

test('getEndpointChainItems falls back to raw endpoint_chain when normalized chain is empty', () => {
  const request = {
    endpointChain: [],
    endpoint_chain: JSON.stringify([
      { channel: 'CCTQ', credential_group: 'default', endpoint: 'Ark API' },
    ]),
  };

  const chain = getEndpointChainItems(request);

  assert.equal(chain.length, 1);
  assert.equal(parseChainNode(chain[0]).channel, 'CCTQ');
  assert.equal(parseChainNode(chain[0]).credentialGroup, 'default');
  assert.equal(parseChainNode(chain[0]).endpoint, 'Ark API');
});

test('chainHasMultipleChannels only reports actual inter-channel paths', () => {
  const sameChannelChain = [
    { channel: 'anyrouter', credential_group: '默认分组', endpoint: 'jp-node' },
    { channel: 'anyrouter', credential_group: '默认分组', endpoint: 'hk-node' },
  ];

  assert.equal(chainHasMultipleChannels(sameChannelChain), false);
  assert.equal(parseChainNode(sameChannelChain[0]).credentialGroup, '默认分组');

  assert.equal(chainHasMultipleChannels([
    { channel: 'anyrouter', credential_group: '默认分组', endpoint: 'hk-node' },
    { channel: 'Right Code', credential_group: 'AWSQ逆向渠道', endpoint: 'AWSQ逆向渠道' },
  ]), true);
});

test('request credential group label prefers the current chain node and keeps Codex group fallback', () => {
  const getLabel = endpointChainUtils.getRequestCredentialGroupLabel;
  const chainNodes = [
    { credentialGroup: '起始分组' },
    { credentialGroup: '订阅' },
  ];

  assert.equal(getLabel?.({ provider: 'codex', group: '旧分组' }, chainNodes), '订阅');
  assert.equal(getLabel?.({ provider: 'codex', credentialGroup: '直接分组' }, []), '直接分组');
  assert.equal(getLabel?.({ provider: 'codex', groupName: '记录分组' }, []), '记录分组');
  assert.equal(getLabel?.({ provider: 'codex', group: '订阅' }, []), '订阅');
  assert.equal(getLabel?.({ provider: 'claude', group: '渠道字段' }, []), '-');
  assert.equal(getLabel?.(
    { provider: 'codex', credentialGroup: '最终记录分组' },
    [{ credentialGroup: '失败旧分组' }, { credentialGroup: '' }],
  ), '最终记录分组');
});

test('request channel label never treats a Codex credential group as its channel', () => {
  const getChannelLabel = endpointChainUtils.getRequestChannelLabel;

  assert.equal(getChannelLabel?.({ provider: 'codex', group: '订阅' }, [], ''), '');
  assert.equal(getChannelLabel?.({ provider: 'claude', group: 'Claude 兼容渠道' }, [], ''), 'Claude 兼容渠道');
  assert.equal(getChannelLabel?.({ provider: 'codex', channel: '合租巴士' }, [], ''), '合租巴士');
});

test('summarizeEndpointAttempts separates retries from adjacent identity transfers', () => {
  assert.deepEqual(summarizeEndpointAttempts({
    retry_count: 3,
    endpoint_chain: [
      { channel: 'CCTQ', credential_group: 'primary', endpoint: 'A' },
      { channel: 'CCTQ', credential_group: 'backup', endpoint: 'B' },
      { channel: 'CCTQ', credential_group: 'backup', endpoint: 'B' },
    ],
  }), {
    attemptCount: 4,
    retryCount: 3,
    transferCount: 1,
    transferred: true,
    firstEndpoint: 'A',
    finalEndpoint: 'B',
  });
});

test('summarizeEndpointAttempts uses chain length as the historical retry floor', () => {
  const summary = summarizeEndpointAttempts({
    retry_count: 0,
    endpoint_chain: 'CCTQ::primary::A -> CCTQ::backup::B',
  });

  assert.equal(summary.attemptCount, 2);
  assert.equal(summary.retryCount, 1);
  assert.equal(summary.transferCount, 1);
});

test('summarizeEndpointAttempts uses the recorded endpoint for direct requests without a chain', () => {
  const summary = summarizeEndpointAttempts({ endpoint: 'direct-endpoint', endpoint_chain: [] });

  assert.equal(summary.firstEndpoint, 'direct-endpoint');
  assert.equal(summary.finalEndpoint, 'direct-endpoint');
  assert.equal(summary.attemptCount, 1);
});

test('network display state preserves unknown duration and streaming values', () => {
  assert.deepEqual(getRequestNetworkDisplayState({}), {
    durationMs: null,
    isStreaming: null,
  });
  assert.deepEqual(getRequestNetworkDisplayState({ duration: 0, isStreaming: false }), {
    durationMs: 0,
    isStreaming: false,
  });
});
