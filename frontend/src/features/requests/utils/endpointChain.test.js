import test from 'node:test';
import assert from 'node:assert/strict';
import { getRequestResourcePath } from './endpointChain.js';

test('resource tooltip follows group and endpoint changes even within the same channel', () => {
  const attempts = [
    { channel: 'A', credentialGroup: 'primary', endpoint: 'one' },
    { channel: 'A', credentialGroup: 'primary', endpoint: 'one' },
    { channel: 'A', credentialGroup: 'backup', endpoint: 'one' },
    { channel: 'A', credentialGroup: 'backup', endpoint: 'two' },
    { channel: 'B', credentialGroup: 'default', endpoint: 'three' },
  ];
  const path = getRequestResourcePath({ channel: 'old-active', dispatchTrace: { attemptCount: 5, attempts, skipped: [{ channel: 'unused' }] } });
  assert.deepEqual(path.map(node => [node.channel, node.credentialGroup, node.endpoint, node.attempts]), [
    ['A', 'primary', 'one', 2], ['A', 'backup', 'one', 1], ['A', 'backup', 'two', 1], ['B', 'default', 'three', 1],
  ]);
  assert.equal(getRequestResourcePath({ channel: 'old-active', endpoint: 'old', dispatchTrace: { attemptCount: 0, attempts: [] } }).length, 0);
});

test('resource tooltip supports direct and legacy calls without assigning final resources to earlier hops', () => {
  const [direct] = getRequestResourcePath({ provider: 'codex', channel: 'A', group: 'plus', endpoint: 'primary' });
  assert.deepEqual([direct.channel, direct.credentialGroup, direct.endpoint], ['A', 'plus', 'primary']);
  const legacy = getRequestResourcePath({ channel: 'final', groupName: 'final-group', endpointChain: ['old', 'next'] });
  assert.deepEqual(legacy.map(node => node.channel), ['', '']);
  assert.deepEqual(legacy.map(node => node.credentialGroup), ['', '']);
  assert.deepEqual(getRequestResourcePath({}), []);
});

import {
  chainHasMultipleChannels,
  collectFailedHops,
  countChannelSwitches,
  extractUpstreamErrorMessage,
  formatUpstreamErrorText,
  getEndpointChainItems,
  getRequestNetworkDisplayState,
  parseChainNode,
  parseEndpointChain,
  parseEndpointName,
  requestHasInterChannelTransfer,
  summarizeEndpointAttempts,
} from './endpointChain.js';
import * as endpointChainUtils from './endpointChain.js';

test('failed hops list every upstream error, newest first, including empty-body statuses', () => {
  const nodes = [
    { hop: 1, endpoint: 'A', statusCode: 401, error: '{"error":{"message":"Invalid token"}}', headers: {} },
    { hop: 2, endpoint: 'B', statusCode: 520, error: '', headers: { server: 'cloudflare' } },
    { hop: 3, endpoint: 'C', statusCode: 0, error: '', headers: {} },
  ];
  // 第 3 跳什么都没记录（进行中或旧记录），不列出；520 空正文仍要列出
  assert.deepEqual(collectFailedHops(nodes).map((node) => node.hop), [2, 1]);
});

test('upstream error text is pretty-printed without altering its content', () => {
  const raw = '{"error":{"message":"由于 claude 模型供应难以保证","type":"error"}}';
  const formatted = formatUpstreamErrorText(raw);
  assert.equal(formatted.isJSON, true);
  assert.deepEqual(JSON.parse(formatted.text), JSON.parse(raw));
  assert.equal(formatUpstreamErrorText('http2: timeout awaiting response headers').isJSON, false);
  assert.equal(formatUpstreamErrorText('{broken').text, '{broken');

  assert.equal(extractUpstreamErrorMessage(raw), '由于 claude 模型供应难以保证');
  assert.equal(extractUpstreamErrorMessage('{"error":"quota exceeded","type":"error"}'), 'quota exceeded');
  assert.equal(extractUpstreamErrorMessage('plain text'), '');
});

test('parseChainNode keeps diagnostic headers and the local-failure flag', () => {
  const node = parseChainNode({ endpoint: 'A', status_code: 520, headers: { server: 'cloudflare', 'cf-ray': 'x1' } });
  assert.deepEqual(node.headers, { server: 'cloudflare', 'cf-ray': 'x1' });
  assert.equal(node.local, false);
  assert.equal(parseChainNode({ endpoint: 'A', error: 'dial tcp: timeout', local: true }).local, true);
});

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
    attempts: 0,
    durationMs: null,
    headers: {},
    local: false,
    hop: 1,
  });
  assert.equal(chainHasMultipleChannels(chain), true);
  // 新链路带每跳耗时；0 是有效值
  assert.equal(parseChainNode({ endpoint: 'A', duration_ms: 1234 }).durationMs, 1234);
  assert.equal(parseChainNode({ endpoint: 'A', duration_ms: 0 }).durationMs, 0);
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

test('parseChainNode never surfaces the internal failure_reason classification as an error', () => {
  const node = parseChainNode({ hop: 1, endpoint: 'Ark API', failure_reason: 'endpoint_exhausted' });

  assert.equal(node.error, '');
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

test('countChannelSwitches counts adjacent channel changes only', () => {
  assert.equal(countChannelSwitches([
    { channel: 'Ciallo', endpoint: 'A' },
    { channel: 'Ciallo', endpoint: 'B' },
    { channel: 'Ark API', endpoint: 'C' },
    { channel: 'Ark API', endpoint: 'D' },
    { channel: 'Ciallo', endpoint: 'E' },
  ]), 2);
  assert.equal(countChannelSwitches([
    { channel: 'Ciallo', endpoint: 'A' },
    { channel: '', endpoint: 'unknown' },
    { channel: 'Ark API', endpoint: 'B' },
  ]), 0);
});

test('Codex multi-channel paths report inter-channel transfer from recorded data', () => {
  assert.equal(requestHasInterChannelTransfer({
    provider: 'codex',
    endpoint_chain: [
      { channel: 'Ciallo', credential_group: '默认分组', endpoint: '主线路' },
      { channel: 'Ark API', credential_group: '默认分组', endpoint: '默认端点' },
      { channel: 'Ciallo', credential_group: '默认分组', endpoint: 'CF优选线路' },
    ],
  }), true);

  assert.equal(requestHasInterChannelTransfer({
    provider: 'codex',
    endpoint_chain: [
      { channel: 'Ciallo', credential_group: '默认分组', endpoint: '主线路' },
      { channel: 'Ciallo', credential_group: '默认分组', endpoint: 'CF优选线路' },
    ],
  }), false);
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

test('request channel labels use actual profile attempts and ignore active or skipped channels', () => {
  for (const provider of ['claude', 'codex']) {
    for (const kind of ['default', 'non_stream']) {
      const request = {
        provider,
        channel: '旧激活渠道',
        dispatchTrace: {
          decision: { kind },
          attemptCount: 2,
          attempts: [{ channel: '方案渠道 A' }, { channel: '实际改道渠道 B' }],
          skipped: [{ channel: '未使用的非流改道渠道' }],
        },
      };
      assert.equal(endpointChainUtils.getRequestChannelLabel(request, [{ channel: '旧链路渠道' }]), '实际改道渠道 B');
      request.dispatchTrace.attempts = [];
      request.dispatchTrace.attemptCount = 0;
      assert.equal(endpointChainUtils.getRequestChannelLabel(request, [{ channel: '旧链路渠道' }]), '待调度');
      request.dispatchTrace.finished = true;
      assert.equal(endpointChainUtils.getRequestChannelLabel(request), '未发送');
      request.dispatchTrace.finished = false;
      request.dispatchTrace.stopReason = 'route_unavailable';
      assert.equal(endpointChainUtils.getRequestChannelLabel(request), '未发送');
    }
  }
});

test('legacy request channel labels prefer the last recorded hop and retain direct fallbacks', () => {
  const request = { provider: 'codex', channel: '旧激活渠道' };
  assert.equal(endpointChainUtils.getRequestChannelLabel(request, [{ channel: '起始渠道' }, { channel: '最终渠道' }]), '最终渠道');
  assert.equal(endpointChainUtils.getRequestChannelLabel(request), '旧激活渠道');
  assert.equal(endpointChainUtils.getRequestChannelLabel({ provider: 'codex' }), '-');
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
    finalEndpointAttempts: null,
    retryCount: 3,
    transferCount: 1,
    transferred: true,
    firstEndpoint: 'A',
    finalEndpoint: 'B',
  });
});

test('summarizeEndpointAttempts separates final-endpoint attempts from the request total', () => {
  // 端点内重试 2 次后转移 1 次：A 发了 3 次，B 发了 1 次
  const summary = summarizeEndpointAttempts({
    retry_count: 3,
    endpoint_chain: [
      { hop: 1, channel: 'CCTQ', endpoint: 'A', attempts: 3, status_code: 503 },
      { hop: 2, channel: 'CCTQ', endpoint: 'B', attempts: 1 },
    ],
  });

  assert.equal(summary.finalEndpointAttempts, 1);
  assert.equal(summary.attemptCount, 4);
  assert.equal(summary.retryCount, 3);
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


test('network summary uses correlated dispatch attempts and preserves an actual zero-send outcome', () => {
  const a = { channel: 'A', credentialGroup: 'key-A', endpoint: 'primary' };
  const b = { channel: 'B', credentialGroup: 'key-B', endpoint: 'fallback' };
  const summary = summarizeEndpointAttempts({ endpoint: 'fallback', dispatchTrace: { attemptCount: 3, attempts: [a, b, b] } });
  assert.equal(summary.attemptCount, 3);
  assert.equal(summary.finalEndpointAttempts, 2);
  assert.equal(summary.retryCount, 2);
  assert.equal(summary.transferCount, 1);
  assert.equal(summary.firstEndpoint, 'primary');
  assert.equal(summary.finalEndpoint, 'fallback');
  const blocked = summarizeEndpointAttempts({ dispatchTrace: { attemptCount: 0, attempts: [] } });
  assert.equal(blocked.attemptCount, 0);
  assert.equal(blocked.retryCount, 0);
  assert.equal(blocked.finalEndpointAttempts, null);
  assert.equal(blocked.firstEndpoint, '');
});
