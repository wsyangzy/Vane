import test from 'node:test';
import assert from 'node:assert/strict';
import {
  buildFailureToast,
  buildSuspensionToast,
  describeFailureReason,
  extractFailureCode,
  failureNeedsManualIntervention,
  getRequestFailureDetails,
  shouldNotifyFailure
} from './failureSummary.js';

test('failure cards separate real upstream replies from local pre-send rejection', () => {
  const upstream = getRequestFailureDetails({ failure_reason: 'auxiliary_endpoint_unsupported', http_status_is_upstream: true, http_status_code: 404,
    last_failure_reason: '{"error":{"message":"Invalid URL"}}', endpoint_chain: [{ endpoint: 'one', attempts: 1, status_code: 404, error: 'Invalid URL' }] });
  assert.equal(upstream.hasUpstream, true);
  assert.equal(upstream.showLocal, false);
  assert.equal(upstream.category, '辅助接口不受支持');
  assert.equal(upstream.upstreamHops[0].error, 'Invalid URL');
  const local = getRequestFailureDetails({ failure_reason: 'account_cooling_down', last_failure_reason: 'account_cooling_down', http_status_is_upstream: false,
    endpoint_chain: [{ endpoint: 'one', local: true, error: 'account_cooling_down' }] });
  assert.equal(local.hasUpstream, false);
  assert.equal(local.localTitle, '请求未发送');
  assert.equal(local.localSummary, '端点处于冷却中');
  assert.equal(local.localDetail, '');
  assert.equal(local.upstreamHops.length, 0);
});

test('mixed failures put local transport details in a separate card', () => {
  const detail = getRequestFailureDetails({ failure_reason: 'endpoint_exhausted', http_status_is_upstream: true, http_status_code: 503,
    endpoint_chain: [{ endpoint: 'one', status_code: 503, attempts: 1, error: 'upstream unavailable' }, { endpoint: 'two', attempts: 1, local: true, error: 'dial tcp: timeout' }] });
  assert.equal(detail.hasUpstream, true);
  assert.equal(detail.showLocal, true);
  assert.equal(detail.localTitle, '未收到上游响应');
  assert.deepEqual(detail.upstreamHops.map(hop => hop.endpoint), ['one']);
  assert.deepEqual(detail.localHops.map(hop => hop.error), ['dial tcp: timeout']);
});

test('search configuration failures and actual service errors remain separate', () => {
  const local = getRequestFailureDetails({ failure_reason: 'search_cached_unsupported', http_status_is_upstream: false });
  assert.equal(local.localTitle, '请求未发送');
  assert.equal(local.hasUpstream, false);
  const upstream = getRequestFailureDetails({ failure_reason: 'search_upstream_error', http_status_is_upstream: true,
    http_status_code: 429, last_failure_reason: '{"detail":"search quota exhausted"}',
    endpoint_chain: [{ endpoint: 'Tavily', attempts: 1, status_code: 429, error: '{"detail":"search quota exhausted"}' }] });
  assert.equal(upstream.showLocal, false);
  assert.equal(upstream.upstreamStatus, 429);
  assert.equal(upstream.upstreamDetail, '{"detail":"search quota exhausted"}');
});

test('old records without upstream evidence do not claim endpoints were all attempted', () => {
  const detail = getRequestFailureDetails({ failure_reason: 'endpoint_exhausted', http_status_is_upstream: false });
  assert.equal(detail.hasUpstream, false);
  assert.equal(detail.showLocal, true);
  assert.equal(detail.localSummary, '未记录具体的本地失败原因');
});

test('local outcomes use warning notifications and response processing stays separate from upstream errors', () => {
  const toast = buildFailureToast({ provider: 'codex', failure_reason: 'account_cooling_down', http_status_is_upstream: false });
  assert.equal(toast.type, 'warning');
  assert.equal(toast.title, 'Codex 请求未发送');
  assert.equal(toast.message[0], '端点处于冷却中');
  assert.ok(toast.message.some(line => line.includes('恢复时间未知')));
  const detail = getRequestFailureDetails({ failure_reason: 'response_read_error', http_status_is_upstream: true, http_status_code: 200, last_failure_reason: 'unexpected EOF' });
  assert.equal(detail.hasUpstream, false);
  assert.equal(detail.localTitle, '本地响应处理失败');
  assert.equal(detail.localDetail, 'unexpected EOF');
  const withChain = getRequestFailureDetails({ failure_reason: 'response_read_error', http_status_is_upstream: true, http_status_code: 200,
    last_failure_reason: 'unexpected EOF', endpoint_chain: [{ endpoint: 'one', attempts: 1, status_code: 200 }] });
  assert.equal(withChain.hasUpstream, false);
  assert.equal(withChain.showLocal, true);
  assert.equal(withChain.localTitle, '本地响应处理失败');
  assert.equal(withChain.upstreamHops.length, 0);
  assert.equal(buildFailureToast({ failure_reason: 'response_read_error', http_status_is_upstream: true, http_status_code: 200 }).type, 'warning');
});

test('a skipped fallback does not hide a real upstream error or imply the whole request was unsent', () => {
  const detail = getRequestFailureDetails({ failure_reason: 'endpoint_exhausted', http_status_is_upstream: true, http_status_code: 503,
    dispatchTrace: { attemptCount: 0 }, endpoint_chain: [{ endpoint: 'one', attempts: 1, status_code: 503, error: 'unavailable' },
      { endpoint: 'two', local: true, error: 'account_cooling_down' }] });
  assert.equal(detail.hasUpstream, true);
  assert.equal(detail.showLocal, true);
  assert.equal(detail.localTitle, '部分目标未发送');
  assert.equal(detail.localSummary, '端点处于冷却中');
});

test('extractFailureCode reads the classification before legacy detail suffixes', () => {
  assert.equal(extractFailureCode('endpoint_exhausted'), 'endpoint_exhausted');
  assert.equal(extractFailureCode('endpoint_exhausted: request failed: Post "https://x/v1/messages": EOF'), 'endpoint_exhausted');
  assert.equal(extractFailureCode('所有渠道均已尝试'), '');
  assert.equal(extractFailureCode(''), '');
});

test('retry budget message identifies Vane as the stop source', () => {
	assert.equal(describeFailureReason('retry_budget_exhausted'), 'Vane 已达到本次请求重试上限并停止继续尝试');
});

test('retry stop reasons distinguish no sends, count limits and elapsed-time limits without exposing raw bodies', () => {
  const recovery = buildFailureToast({ provider: 'codex', failure_reason: 'recovery_outside_budget', http_status_is_upstream: false,
    last_failure_reason: '<html>private upstream content</html>',
    endpoint_chain: [{ local: true, endpoint: 'one', error: 'account_cooling_down' }],
    route_snapshot: JSON.stringify({ attempts: 0, next_recovery_at: '2026-11-17T01:00:00Z' }) });
  assert.equal(recovery.title, 'Codex 请求未发送');
  assert.match(recovery.message[0], /预计恢复时间晚于本次请求剩余等待时限/);
  assert.match(recovery.message.at(-1), /查看相关端点的最近失败记录/);
  assert.ok(recovery.message.includes('实际向上游发送 0 次'));
  assert.ok(recovery.message.some(line => line.startsWith('预计恢复时间：')));
  assert.ok(!recovery.message.join().includes('private upstream content'));
  const sent = buildFailureToast({ provider: 'claude', failure_reason: 'retry_attempts_exhausted', http_status_is_upstream: false,
    route_snapshot: { attempts: 3 }, endpoint_chain: [{ local: true, attempts: 3, endpoint: 'one', error: 'connection reset' }] });
  assert.equal(sent.title, 'Claude 未收到上游响应');
  assert.ok(sent.message.includes('实际向上游发送 3 次'));
  assert.match(describeFailureReason('retry_deadline_exceeded'), /总时长/);
  for (const code of ['retry_attempts_exhausted', 'retry_deadline_exceeded', 'recovery_outside_budget']) assert.equal(shouldNotifyFailure(code), true);
});

test('recovery stop explains candidate blockers and recommends a matching next action', () => {
  const request = {
    provider: 'codex', failure_reason: 'recovery_outside_budget', http_status_is_upstream: false,
    route_snapshot: { attempts: 0, next_recovery_at: '2026-11-17T11:04:39+08:00', candidates: [
      { action: 'skipped', reason: 'cooldown', candidate: { channel: '41', endpoint: '109' } },
      { action: 'skipped', reason: 'daily_cost_limit_exceeded', candidate: { channel: '38', endpoint: '97' } },
      { action: 'skipped', reason: 'daily_cost_limit_exceeded', candidate: { channel: '38', endpoint: '98' } },
    ] },
  };
  const details = getRequestFailureDetails(request);
  assert.equal(details.localTitle, '请求未发送');
  assert.deepEqual(details.blockingCandidates.map(item => [item.reason, item.count]), [
    ['cooldown', 1], ['daily_cost_limit_exceeded', 2],
  ]);
  assert.match(details.blockerAdvice, /最近一次失败记录/);
  assert.match(details.blockerAdvice, /等待限额重置/);
  const toast = buildFailureToast(request);
  assert.ok(toast.message.some(line => /健康冷却中（1 个候选）/.test(line)));
  assert.ok(toast.message.some(line => /每日成本限额（2 个候选）/.test(line)));
  assert.ok(toast.message.some(line => line.includes('编辑此凭证分组')));

  const resolved = getRequestFailureDetails({ ...request, dispatchTrace: { skipped: [
    { channel: 'ZeroCat', credentialGroup: '混合池', endpoint: '主站', reason: 'daily_cost_limit_exceeded' },
  ] } });
  assert.equal(resolved.blockingCandidates[0].channel, 'ZeroCat');
  assert.equal(resolved.blockingCandidates[0].credentialGroup, '混合池');
  assert.deepEqual(resolved.blockingCandidates[0].endpoints, ['主站']);
});

test('upstream failure details also show skipped route candidates and their reasons', () => {
  const details = getRequestFailureDetails({
    failure_reason: 'incomplete_stream', http_status_is_upstream: true, http_status_code: 200,
    endpoint_chain: [{ channel: 'NewCodex', endpoint: '默认', attempts: 1, status_code: 200, error: '<html>diagnostic response</html>' }],
    dispatchTrace: { attemptCount: 1, skipped: [{ channel: 'ZeroCat', credentialGroup: '混合池', endpoint: '主站', reason: 'daily_cost_limit_exceeded' }] },
  });
  assert.equal(details.hasUpstream, true);
  assert.equal(details.showLocal, true);
  assert.equal(details.localTitle, '部分目标未发送');
  assert.equal(details.blockingCandidates[0].channel, 'ZeroCat');
  assert.match(details.localSummary, /部分候选未发送/);
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
