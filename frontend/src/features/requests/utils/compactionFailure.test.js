import test from 'node:test';
import assert from 'node:assert/strict';
import { buildFailureToast, describeFailureReason, getRequestFailureDetails, getRequestProtocolDiagnostics } from './failureSummary.js';
import { createFailureNotificationAggregator } from './failureNotifications.js';

const upstreamFailure = (error) => ({ provider: 'codex', failure_reason: 'invalid_request', http_status_code: 400,
  http_status_is_upstream: true, last_failure_reason: JSON.stringify({ error }),
  endpoint_chain: [{ channel: 'anyrouter', endpoint: 'main', attempts: 1, status_code: 400, error: JSON.stringify({ error }) }] });

test('context limit errors have actionable Chinese explanations while preserving raw provider text', () => {
  const request = upstreamFailure({ code: 'context_length_exceeded', message: 'maximum context length exceeded' });
  const details = getRequestFailureDetails(request);
  assert.equal(details.upstreamDetail, request.last_failure_reason);
  assert.equal(details.upstreamExplanation.summary, '会话内容太长，超出了当前模型一次能处理的范围');
  assert.match(details.upstreamExplanation.advice, /压缩会话/);
  assert.deepEqual(buildFailureToast(request, details).message, [details.upstreamExplanation.summary, details.upstreamExplanation.advice]);
  assert.equal(describeFailureReason('input_too_long'), details.upstreamExplanation.summary);
});

test('invalid tool type mentions compaction only with recorded compaction evidence', () => {
  const request = upstreamFailure({ code: 'invalid_responses_request', type: 'new_api_error', param: '',
    message: 'invalid tool type (request id: 20261120143034464766768eg6nJide)' });
  assert.doesNotMatch(getRequestFailureDetails(request).upstreamExplanation.advice, /压缩/);
  request.context_summary = JSON.stringify({ parts: { summary: { breakdown: [{ kind: 'compaction' }] } } });
  const details = getRequestFailureDetails(request);
  assert.match(details.upstreamExplanation.advice, /普通摘要不会改变工具格式/);
  assert.match(details.upstreamExplanation.advice, /不能确认压缩状态是原因/);
  assert.match(details.upstreamExplanation.advice, /已有压缩状态不会自动转换/);
  assert.equal(details.upstreamDetail, request.last_failure_reason);
});

test('protocol diagnostics distinguish current and historical tools and omit unknown type values', () => {
  const request = upstreamFailure({ message: 'invalid tool type' });
  request.contextSummary = { protocol: { compaction_items: 1, request_tools: 2, additional_tools: 1,
    request_tool_types: { function: 1, custom: 1, 'private schema': 1 },
    additional_tool_types: { namespace: 1, function: 4 },
    input_types: { message: 1, compaction: 1, additional_tools: 1, 'private prompt': 1 },
    tool_types: { function: 5, custom: 1, namespace: 1 } } };
  const lines = getRequestProtocolDiagnostics(request);
  assert.match(lines[0], /会话压缩项：1 项/);
  assert.match(lines[1], /本次工具声明：2 项（function × 1、custom × 1）/);
  assert.match(lines[2], /历史工具声明（additional_tools）：1 项（function × 4、namespace × 1）/);
  assert.doesNotMatch(lines.join('\n'), /private/);
  assert.match(getRequestFailureDetails(request).upstreamExplanation.advice, /普通摘要/);
  delete request.contextSummary.protocol;
  assert.deepEqual(getRequestProtocolDiagnostics(request), []);
});

test('historical tools remain relevant with zero top-level tools and do not recommend selecting summary again', () => {
  const request = upstreamFailure({ code: 'invalid_responses_request', type: 'new_api_error', param: '',
    message: 'invalid tool type (request id: 2026112017001084336998NqhjLdWi)' });
  request.contextSummary = { protocol: { compaction_items: 1, request_tools: 0, additional_tools: 1,
    additional_tool_types: { function: 10, custom: 1, namespace: 3, 'private tool': 1 },
    input_types: { message: 14, additional_tools: 1, compaction: 1, configuration_update: 1 } } };
  const details = getRequestFailureDetails(request);
  assert.match(details.upstreamExplanation.advice, /function、custom、namespace/);
  assert.match(details.upstreamExplanation.advice, /未指出具体被拒绝的类型/);
  assert.match(details.upstreamExplanation.advice, /配置已应用并重启/);
  assert.match(details.upstreamExplanation.advice, /新会话/);
  assert.doesNotMatch(details.upstreamExplanation.advice, /选择|切换|private tool/);
  assert.equal(details.upstreamDetail, request.last_failure_reason);
  assert.deepEqual(buildFailureToast(request, details).message, [details.upstreamExplanation.summary, details.upstreamExplanation.advice]);
});

test('current tools and explicit zero compaction count take precedence over old summary evidence', () => {
  const request = upstreamFailure({ message: 'invalid tool type' });
  request.contextSummary = { protocol: { compaction_items: 0, request_tool_types: { custom: 1 } },
    parts: { summary: { breakdown: [{ kind: 'compaction' }] } } };
  const advice = getRequestFailureDetails(request).upstreamExplanation.advice;
  assert.match(advice, /custom/);
  assert.doesNotMatch(advice, /压缩|普通摘要/);
});

test('older and malformed summaries do not crash or imply that a compaction item was present', () => {
  const request = upstreamFailure({ message: 'invalid tool type' });
  for (const context_summary of ['{', 'null', '[]', JSON.stringify({ parts: { summary: { breakdown: {} } } })]) {
    const input = { ...request, context_summary };
    assert.doesNotMatch(getRequestFailureDetails(input).upstreamExplanation.advice, /压缩/);
    assert.deepEqual(getRequestProtocolDiagnostics(input), []);
  }
});

test('context limits are explained for typed, nested and plain provider errors', () => {
  for (const error of [
    { type: 'context_length_exceeded', message: 'too long' },
    { code: 'input_too_long', message: 'too long' },
    { message: "This model's maximum context length is 128000 tokens" },
  ]) {
    assert.equal(getRequestFailureDetails(upstreamFailure(error)).upstreamExplanation.code, 'context_length_exceeded');
  }
  const raw = JSON.stringify({ response: { error: { code: 'context_length_exceeded', message: 'too long' } } });
  const request = { ...upstreamFailure({}), endpoint_chain: [], last_failure_reason: raw };
  assert.equal(getRequestFailureDetails(request).upstreamExplanation.code, 'context_length_exceeded');
  assert.equal(getRequestFailureDetails(request).upstreamDetail, raw);
});

test('known upstream errors notify separately despite generic invalid_request classification', () => {
  const aggregator = createFailureNotificationAggregator();
  const context = upstreamFailure({ code: 'context_length_exceeded', message: 'too long' });
  const tools = upstreamFailure({ code: 'invalid_responses_request', message: 'invalid tool type' });
  assert.equal(aggregator.consume([context, tools], 1000).length, 2);
  assert.equal(aggregator.consume([upstreamFailure({ code: 'unrelated', message: 'other 400' })], 1100).length, 0);
  const local = { ...context, http_status_is_upstream: false, http_status_code: 0, endpoint_chain: [] };
  assert.equal(getRequestFailureDetails(local).upstreamExplanation, null);
});
