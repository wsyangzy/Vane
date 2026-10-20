import test from 'node:test';
import assert from 'node:assert/strict';
import {
  buildFailureToast,
  buildSuspensionToast,
  describeFailureReason,
  extractFailureCode,
  failureNeedsManualIntervention,
  shouldNotifyFailure
} from './failureSummary.js';

test('extractFailureCode reads the classification before legacy detail suffixes', () => {
  assert.equal(extractFailureCode('endpoint_exhausted'), 'endpoint_exhausted');
  assert.equal(extractFailureCode('endpoint_exhausted: request failed: Post "https://x/v1/messages": EOF'), 'endpoint_exhausted');
  assert.equal(extractFailureCode('所有渠道均已尝试'), '');
  assert.equal(extractFailureCode(''), '');
});

test('shouldNotifyFailure only fires for terminal or local rejections', () => {
  assert.equal(shouldNotifyFailure('endpoint_exhausted'), true);
  assert.equal(shouldNotifyFailure('daily_cost_limit_exceeded: All groups reached limit'), true);
  assert.equal(shouldNotifyFailure('client_cancelled'), false);
  assert.equal(shouldNotifyFailure('server_error'), false);
  assert.equal(shouldNotifyFailure('已尝试 0 个端点，所有渠道均已尝试'), true);
});

test('failureNeedsManualIntervention flags loop and restart limits', () => {
  assert.equal(failureNeedsManualIntervention('max_loops_exceeded'), true);
  assert.equal(failureNeedsManualIntervention('endpoint_exhausted'), false);
  assert.equal(failureNeedsManualIntervention('达到最大循环轮数'), true);
});

test('buildFailureToast summarises the tried scope instead of naming one endpoint', () => {
  const toast = buildFailureToast({
    requestId: 'req-3e3d0e88',
    provider: 'claude',
    channel: 'anyrouter',
    model: 'claude-fable-5-1',
    httpStatusCode: 429,
    failure_reason: 'endpoint_exhausted',
    last_failure_reason: '{"error":{"message":"Service Unavailable","type":"error"},"type":"error"}',
    endpointChain: [
      { hop: 1, channel: 'anyrouter', credential_group: '默认分组', endpoint: '主站直连' },
      { hop: 2, channel: 'anyrouter', credential_group: 'github', endpoint: '主站直连' },
      { hop: 3, channel: 'anyrouter', credential_group: 'edu', endpoint: '主站直连' },
      { hop: 4, channel: 'anyrouter', credential_group: 'edu', endpoint: '备用' }
    ]
  });
  assert.equal(toast.type, 'error');
  assert.equal(toast.title, 'Claude 请求失败');
  assert.deepEqual(toast.message, [
    '当前渠道端点已全部尝试失败',
    '渠道 anyrouter，已尝试 3 个分组、2 个端点，共 4 次'
  ]);
  // 上游报错、请求 ID、模型属于排查细节，不进提示
  const joined = toast.message.join('\n');
  assert.ok(!joined.includes('Service Unavailable') && !joined.includes('429') && !joined.includes('req-') && !joined.includes('claude-fable'));
});

test('buildFailureToast names the single route when only one endpoint was tried', () => {
  const toast = buildFailureToast({
    provider: 'claude',
    channel: 'anyrouter',
    failure_reason: 'no_supported_model_endpoint',
    endpointChain: [{ hop: 1, channel: 'anyrouter', credential_group: '默认分组', endpoint: '主站直连' }]
  });
  assert.deepEqual(toast.message, [
    '当前渠道没有支持该模型的端点，请求未转发到提供商',
    '渠道 anyrouter · 分组 默认分组 · 端点 主站直连'
  ]);

  const multiChannel = buildFailureToast({
    provider: 'codex',
    failure_reason: 'all_channels_unavailable',
    endpointChain: [
      { channel: 'A', credential_group: 'g1', endpoint: 'e1' },
      { channel: 'B', credential_group: 'g2', endpoint: 'e2' }
    ]
  });
  assert.equal(multiChannel.message[1], '已尝试 2 个渠道、2 个分组、2 个端点，共 2 次');
});

test('buildFailureToast uses warning for manual intervention and survives empty records', () => {
  const manual = buildFailureToast({ provider: 'codex', failure_reason: 'max_loops_exceeded' });
  assert.equal(manual.type, 'warning');
  assert.equal(manual.title, 'Codex 请求等待处理');
  assert.equal(manual.message[0], describeFailureReason('max_loops_exceeded'));

  const empty = buildFailureToast({});
  assert.equal(empty.type, 'error');
  assert.deepEqual(empty.message, ['服务暂时不可用，请稍后重试']);
});

test('buildSuspensionToast lists the endpoints or channels involved', () => {
  const suspended = buildSuspensionToast({ suspendedCount: 3, endpointCounts: { 主站直连: 2, 备用: 1 } });
  assert.equal(suspended.type, 'info');
  assert.deepEqual(suspended.message, [
    '3 个请求已挂起，等待端点恢复或渠道切换',
    '端点 主站直连：2 个请求',
    '端点 备用：1 个请求'
  ]);

  const manual = buildSuspensionToast({ manualRetryCount: 1, channelCounts: { anyrouter: 1 } });
  assert.equal(manual.type, 'warning');
  assert.equal(manual.message[1], '渠道 anyrouter：1 个请求');

  assert.equal(buildSuspensionToast({}), null);
});
