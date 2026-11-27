// ============================================
// 失败摘要 - 请求失败的统一文案构造
// 供右上角全局提示与请求详情弹窗共用，保证两处说法一致。
// 仅依赖请求工具模块，可直接被 node --test 加载。
// ============================================

import { getEndpointChainItems, parseChainNode } from './endpointChain.js';
import { blockageLabel, describeBlockageRecovery, isHealthCooldown, mapBlockage } from './blockage.js';

// failure_reason 是 Relay 的内部分类；这里给每个分类一句用户能看懂的说明。
// last_failure_reason may contain provider text or a local failure; provenance
// and endpoint-chain evidence decide which card owns it.
export const FAILURE_REASON_DETAILS = Object.freeze({
  routing_config_unavailable: '调度配置加载失败，请根据下方具体错误修复后重试',
  routing_profile_missing: '未找到可用的默认调度方案，或调度配置未能加载；请检查调度方案和应用日志',
  route_unavailable: '当前规则与默认编排没有可发送的目标，请检查候选状态和恢复时间',
  no_healthy_endpoints: '当前渠道没有可用端点，请求未转发到提供商',
  no_supported_model_endpoint: '当前渠道没有支持该模型的端点，请求未转发到提供商',
  daily_cost_limit_exceeded: '凭证分组已达每日成本限额，请求未转发到提供商',
  daily_cost_state_unavailable: '暂时无法读取每日成本，请求未转发到提供商',
  retry_budget_exhausted: 'Vane 已达到本次请求重试上限并停止继续尝试',
  retry_attempts_exhausted: '本次请求已达到上游发送次数上限',
  retry_deadline_exceeded: '本次请求已达到允许的总时长',
  recovery_outside_budget: '端点预计恢复时间超出本次请求的剩余等待时间',
  suspension_timeout: '挂起等待端点恢复超时',
  all_channels_unavailable: '所有渠道均已尝试，没有可用的故障转移渠道',
  max_loops_exceeded: '已达最大循环轮数，等待手动重试',
  max_restarts_exceeded: '已达最大重启次数，请求终止',
  endpoint_exhausted: '当前渠道端点已全部尝试失败',
  route_exhausted: '模型路由规则的全部目标均已尝试失败',
  non_stream_route_exhausted: '非流改道的全部目标均已尝试失败',
  stream_truncated: '上游流式响应中途截断',
  incomplete_stream: '上游未返回有效内容或流式结束事件',
  incomplete_response: '上游响应未完整结束',
  non_stream_response: '流式请求收到普通 JSON 响应',
  empty_stream: '上游返回空的流式响应',
  invalid_stream_event: '上游流式事件格式无效',
  credential_rejected: '上游拒绝此分组的凭证',
  balance_exhausted: '上游账号余额已耗尽，请核对余额并充值，或选择其他凭证分组',
  permission_denied: '此凭证没有本次模型或接口的访问权限',
  quota_exhausted: '供应商额度已耗尽，请检查额度与重置时间',
  permission_scope_unknown: '上游拒绝此请求，需核对账号余额和访问权限',
  endpoint_unavailable: '上游端点暂不可用',
  model_not_found: '上游未提供此模型',
  context_length_exceeded: '会话内容太长，超出了当前模型一次能处理的范围',
  invalid_encrypted_content: '上游拒绝了会话中的加密历史内容',
  input_too_long: '会话内容太长，超出了当前模型一次能处理的范围',
  upstream_unavailable: '上游服务暂不可用',
  response_read_error: '上游响应正文读取失败',
  response_write_error: '响应写回客户端失败',
  privacy_filter_failed: '隐私防护处理失败',
  server_error: '上游返回服务端错误',
  rate_limited: '上游限流',
  auth_error: '上游鉴权失败',
  network_error: '网络连接失败',
  timeout: '请求超时',
  client_cancelled: '客户端取消请求',
  account_cooling_down: '端点处于冷却中',
  account_disabled: '凭证分组已停用',
  account_missing: '凭证分组不存在',
  account_repair_required: '凭证需要修复',
  account_probe_in_progress: '端点正在进行恢复探测',
  auxiliary_endpoint_unsupported: '上游不支持此辅助接口',
  image_endpoint_unsupported: '上游不支持此图像接口',
  image_streaming_unsupported: '图像接口目前支持非流式转发',
  search_not_configured: '搜索服务尚未配置或配置不可用',
  search_request_invalid: '搜索请求格式或筛选条件无效',
  search_command_unsupported: '此操作需要原生搜索线路',
  search_cached_unsupported: '搜索适配不提供官方缓存索引，请使用原生线路或 live 搜索',
});

const RETRY_STOP_REASONS = new Set(['retry_budget_exhausted', 'retry_attempts_exhausted', 'retry_deadline_exceeded', 'recovery_outside_budget']);
const COOLDOWN_ROUTE_STOPS = new Set(['route_unavailable', 'no_healthy_endpoints', ...RETRY_STOP_REASONS]);
const retryContext = (request) => {
  const snapshot = routeSnapshot(request);
  const trace = request.dispatchTrace || {};
  const nextRecoveryAt = trace.nextRecoveryAt || snapshot?.next_recovery_at || snapshot?.nextRecoveryAt || '';
  const rawAttempts = trace.attemptCount ?? snapshot?.attempts;
  const savedStop = snapshot?.stop_reason || snapshot?.stopReason || '';
  const attempts = rawAttempts == null ? nextRecoveryAt || RETRY_STOP_REASONS.has(savedStop) ? 0 : null : Math.max(0, Number(rawAttempts) || 0);
  return { attempts, nextRecoveryAt, maxAttempts: Number(trace.maxAttempts ?? snapshot?.max_attempts ?? snapshot?.maxAttempts) || 0,
    maxDurationMs: Number(trace.maxDurationMs ?? snapshot?.max_duration_ms ?? snapshot?.maxDurationMs) || 0 };
};

const retryContextLines = (request) => {
  const { attempts, nextRecoveryAt, maxAttempts, maxDurationMs } = retryContext(request);
  const recovery = nextRecoveryAt && new Date(nextRecoveryAt);
  const limits = [maxAttempts > 0 && `最多发送 ${maxAttempts} 次`, maxDurationMs > 0 && `总时长上限 ${Number((maxDurationMs / 1000).toFixed(2))} 秒`].filter(Boolean);
  return [attempts !== null && `实际向上游发送 ${attempts} 次`,
    limits.length > 0 && `本次请求限制：${limits.join('，')}`,
    recovery && Number.isFinite(recovery.getTime()) && `预计恢复时间：${recovery.toLocaleString('zh-CN', { hour12: false })}`].filter(Boolean);
};

const BLOCKER_LABELS = Object.freeze({
  cooldown: '端点仍处于健康冷却中',
  cooling_down: '端点仍处于健康冷却中',
  account_cooling_down: '端点仍处于健康冷却中',
  daily_cost_limit_exceeded: '凭证分组已达每日成本限额',
  daily_cost_state_unavailable: '暂时无法读取凭证分组的每日成本',
  account_disabled: '凭证分组已停用',
  account_repair_required: '凭证需要修复',
  credential_repair_required: '凭证需要修复',
});

const routeSnapshot = (request) => {
  let snapshot = request.routeSnapshot || request.route_snapshot || {};
  if (typeof snapshot === 'string') { try { snapshot = JSON.parse(snapshot); } catch { snapshot = {}; } }
  return snapshot && typeof snapshot === 'object' && !Array.isArray(snapshot) ? snapshot : {};
};

export const skippedCandidates = (request) => {
  const trace = request.dispatchTrace;
  const traced = Array.isArray(trace?.skipped) ? [...trace.skipped, ...(trace.attempts || [])
    .filter(item => item.action === 'failed' && item.blockage && item.blockage.reason !== item.reason)
    .map(item => ({ ...item, reason: item.blockage.reason }))] : [];
  if (traced.length) return traced;
  const events = routeSnapshot(request).candidates;
  const skipped = (Array.isArray(events) ? events : [])
    .filter(event => (event?.action === 'skipped' || event?.action === 'failed' && event.blockage && event.blockage.reason !== event.reason) && event.candidate)
    .map(event => ({ ...event.candidate, action: event.action,
      reason: event.reason === 'candidate_duplicate' ? event.reason : event.blockage?.reason || event.reason || '', blockage: event.blockage }));
  if (skipped.length) return skipped;
  const reason = extractFailureCode(request.failure_reason || request.failureReason || '');
  return reason.startsWith('daily_cost_') || reason.startsWith('account_') || reason.startsWith('manual_balance_') || reason.startsWith('subscription_')
    ? [{ channel: request.channel || '', credentialGroup: request.credentialGroup || request.group || request.groupName || request.group_name || '',
      endpoint: request.endpoint || request.endpoint_name || '', reason }]
    : [];
};

const groupedBlockers = (request) => {
  const groups = new Map();
  for (const item of skippedCandidates(request)) {
    const reason = item.reason || '';
    if (!reason || reason === 'candidate_duplicate') continue;
    const candidate = item.candidate || item;
    const blockage = mapBlockage(item.blockage) || mapBlockage({ provider: request.provider,
      channel_id: item.channelId || (/^\d+$/.test(candidate.channel || '') ? candidate.channel : 0),
      group_id: item.groupId || String(candidate.credential || '').match(/^\d+\/(\d+)$/)?.[1] || 0,
      endpoint_id: item.endpointId || (/^\d+$/.test(candidate.endpoint || '') ? candidate.endpoint : ''),
      reason, model: item.model || candidate.model || '' });
    const label = BLOCKER_LABELS[reason] || describeFailureReason(reason) || blockageLabel(reason);
    const channel = blockage?.channelName || item.channel || candidate.channel_name || candidate.channel || '';
    const credentialGroup = blockage?.groupName || item.credentialGroup || item.credential_group || candidate.group_name || candidate.credential_group || '';
    const endpoint = blockage?.endpointName || item.endpoint || candidate.endpoint_name || candidate.endpoint || '';
    const normalize = (value, fallback) => !value ? fallback : /^\d+$/.test(String(value)) ? `${fallback} #${value}` : String(value);
    const key = JSON.stringify([reason, blockage?.channelId || channel, blockage?.groupId || credentialGroup, blockage?.model, blockage?.cause, blockage?.recoverAt]);
    let group = groups.get(key);
    if (!group) {
      group = { reason, label, channel: normalize(channel, '渠道'), credentialGroup, endpoints: new Set(), candidates: new Set(), count: 0,
        hasSkipped: false,
        blockage: blockage || mapBlockage({ reason }), recovery: describeBlockageRecovery(blockage || { reason }) };
      groups.set(key, group);
    }
    group.candidates.add(JSON.stringify([blockage?.endpointId || endpoint, item.model || candidate.model || '']));
    group.hasSkipped ||= item.action !== 'failed';
    group.count = group.candidates.size;
    if (endpoint) group.endpoints.add(normalize(endpoint, '端点'));
  }
  return [...groups.values()].map(({ candidates: _candidates, ...group }) => ({ ...group, endpoints: [...group.endpoints] }));
};

const blockerAdvice = (code, blockers) => {
  if (!blockers.length) return code === 'recovery_outside_budget'
    ? '建议：查看相关端点的最近失败记录和恢复时间，待端点恢复后重试。'
    : '';
  const reasons = new Set(blockers.map(item => item.reason));
  const advice = [];
  if (reasons.has('cooldown') || reasons.has('cooling_down') || reasons.has('account_cooling_down')) {
    advice.push('检查冷却端点最近一次失败记录，修复问题或等待冷却结束后重试');
  }
  if (reasons.has('daily_cost_limit_exceeded')) advice.push('检查已达每日限额的分组，等待限额重置或调整分组限额');
  if (!advice.length) advice.push('检查上述候选的可用状态，恢复至少一个候选后重试');
  return `建议：${advice.join('；')}。`;
};

// A provider reply and a local scheduling/transport outcome have different
// evidence. Never render an internal classification as the provider's words.
export const getRequestFailureDetails = (request = {}) => {
  const nodes = getEndpointChainItems(request).map((item, index) => parseChainNode(item, index + 1));
  const localHops = nodes.filter(node => node.local || node.skipReason).reverse();
  const status = Number(request.http_status_code ?? request.httpStatusCode ?? request.status_code ?? request.httpStatus ?? 0) || 0;
  const source = request.http_status_is_upstream ?? request.httpStatusIsUpstream;
  const reason = request.failure_reason || request.failureReason || '';
  const reasonCode = extractFailureCode(reason);
  const blockingCandidates = groupedBlockers(request);
  const hasSkippedCandidates = blockingCandidates.some(item => item.hasSkipped);
  const savedStop = routeSnapshot(request).stop_reason || routeSnapshot(request).stopReason || request.dispatchTrace?.stopReason || '';
  const stopReason = RETRY_STOP_REASONS.has(extractFailureCode(reason)) ? reason : savedStop;
  const retryStop = RETRY_STOP_REASONS.has(extractFailureCode(stopReason));
  const retry = retryContext(request);
  const processingFailure = ['response_read_error', 'response_write_error', 'privacy_filter_failed'].includes(reason);
  const upstreamHops = nodes.filter(node => !node.local && node.statusCode > 0 && (!processingFailure || node.statusCode >= 400)).reverse();
  const hasUpstream = upstreamHops.length > 0 || source !== false && status > 0 && (!processingFailure || status >= 400);
  const raw = String(request.last_failure_reason || request.lastFailureReason || '');
  const detail = raw !== reason ? raw : '';
  const upstreamExplanation = hasUpstream ? explainUpstreamFailure(upstreamHops[0]?.error || detail || reason, request) : null;
  const skipReason = localHops.find(node => node.skipReason)?.skipReason || '';
  const admissionReason = /^(account_|subscription_|state_|profile_|routing_|candidate_)/.test(reason)
    || reason.startsWith('search_') || reason === 'daily_cost_state_unavailable'
    || ['no_healthy_endpoints', 'no_supported_model_endpoint', 'daily_cost_limit_exceeded', 'privacy_filter_failed', 'image_streaming_unsupported'].includes(reason);
  const sent = nodes.some(node => !node.skipReason && (node.attempts > 0 || node.statusCode > 0 || !node.local && node.error)) || retry.attempts > 0;
  const unsent = !hasUpstream && !sent && (Boolean(skipReason) || retry.attempts === 0 || admissionReason);
  const cooldownBlocked = unsent && (isHealthCooldown(reasonCode) || COOLDOWN_ROUTE_STOPS.has(reasonCode) || !reasonCode) && (
    blockingCandidates.length > 0 ? blockingCandidates.every(item => isHealthCooldown(item.reason)) : isHealthCooldown(skipReason || reasonCode)
  );
  return {
    upstreamHops, localHops, hasUpstream, cooldownBlocked, upstreamExplanation, nextRecoveryAt: retry.nextRecoveryAt,
    upstreamStatus: hasUpstream ? status : 0,
    upstreamDetail: hasUpstream ? processingFailure ? upstreamHops[0]?.error || '' : detail : '',
    showLocal: retryStop || processingFailure || localHops.length > 0 || blockingCandidates.length > 0 || !hasUpstream,
    localTitle: unsent ? '请求未发送' : retryStop && hasUpstream ? '重试已停止' : processingFailure ? '本地响应处理失败'
      : hasUpstream && (localHops.some(hop => hop.skipReason) || hasSkippedCandidates) ? '部分目标未发送'
        : hasUpstream && blockingCandidates.length ? '调用失败后的恢复条件' : '未收到上游响应',
    localSummary: reasonCode === 'recovery_outside_budget'
      ? '候选端点的最早重试时间超出本次请求的等待上限。'
      : cooldownBlocked ? '候选端点仍在冷却，本次请求在 Vane 内结束。'
      : hasUpstream && blockingCandidates.length > 0
        ? hasSkippedCandidates ? '部分候选未发送；下方列出调度记录中的跳过原因。' : '本次上游调用失败后，以下候选进入阻塞状态。'
      : ['endpoint_exhausted', 'route_exhausted', 'non_stream_route_exhausted', 'all_channels_unavailable'].includes(reason) && !skipReason
      ? localHops.some(hop => hop.error) || detail ? '连接或响应传输未完成' : '未记录具体的本地失败原因'
      : describeFailureReason(retryStop ? stopReason : skipReason || reason) || (unsent ? '本地调度未选出可发送的目标' : '本次请求没有可用的上游响应记录'),
    localDetail: processingFailure || !hasUpstream ? detail : '',
    stopContext: retryStop || cooldownBlocked ? retryContextLines(request) : [],
    blockingCandidates,
    blockerAdvice: blockerAdvice(reasonCode, blockingCandidates),
    category: reason === 'auxiliary_endpoint_unsupported' ? '辅助接口不受支持' : reason === 'image_endpoint_unsupported' ? '图像接口不受支持' : '',
  };
};

// 需要用户介入（激活渠道、点重试）的分类用 warning，其余终态失败用 error。
const MANUAL_INTERVENTION_REASONS = new Set(['max_loops_exceeded', 'max_restarts_exceeded']);

// 只对「整条链路都失败」或「Relay 本地拒绝」的终态弹提示；
// 单端点失败会被故障转移接住，弹出来只会淹没真正需要处理的提示。
const NOTIFY_REASONS = new Set([
  'invalid_encrypted_content',
  'routing_config_unavailable',
  'routing_profile_missing',
  'route_unavailable',
  'all_channels_unavailable',
  'endpoint_exhausted',
  'route_exhausted',
  'non_stream_route_exhausted',
  'max_loops_exceeded',
  'max_restarts_exceeded',
  'no_healthy_endpoints',
  'no_supported_model_endpoint',
  'daily_cost_limit_exceeded',
  'daily_cost_state_unavailable',
  'retry_budget_exhausted',
  'retry_attempts_exhausted',
  'retry_deadline_exceeded',
  'recovery_outside_budget',
  'suspension_timeout',
  'account_cooling_down',
  'account_disabled',
  'account_missing',
  'account_repair_required',
  'route_unavailable',
  'auxiliary_endpoint_unsupported',
  'context_length_exceeded',
  'input_too_long',
]);

// 旧记录的 failure_reason 形如 "endpoint_exhausted: request failed: ..."，冒号前才是分类。
export const extractFailureCode = (failureReason) => {
  if (typeof failureReason !== 'string') return '';
  const head = failureReason.trim().split(':')[0].trim().toLowerCase();
  return /^[a-z][a-z0-9_]*$/.test(head) ? head : '';
};

// Keep the provider's body intact. This explanation is a separate display layer,
// including for older records classified only as invalid_request.
const requestContextSummary = (request) => {
  let summary = request.contextSummary || request.context_summary || {};
  if (typeof summary === 'string') { try { summary = JSON.parse(summary); } catch { summary = {}; } }
  return summary && typeof summary === 'object' && !Array.isArray(summary) ? summary : {};
};

// Only display the protocol vocabulary recorded by the backend. Type names
// outside the allowlist must not turn diagnostics into a request-text surface.
const INPUT_PROTOCOL_TYPES = ['message', 'reasoning', 'function_call', 'function_call_output', 'custom_tool_call',
  'custom_tool_call_output', 'tool_search_call', 'tool_search_output', 'additional_tools', 'compaction',
  'compaction_summary', 'compaction_trigger', 'configuration_update', 'other'];
const TOOL_PROTOCOL_TYPES = ['function', 'custom', 'namespace', 'web_search', 'web_search_preview', 'tool_search',
  'mcp', 'file_search', 'code_interpreter', 'image_generation', 'computer', 'computer_use_preview', 'local_shell', 'shell', 'apply_patch', 'other'];
const protocolCount = (value) => Number.isSafeInteger(value) && value >= 0 ? value : 0;
const protocolTypes = (counts, allowed) => allowed.filter(kind => protocolCount(counts?.[kind]) > 0)
  .map(kind => `${kind} × ${counts[kind]}`).join('、');

export const getRequestProtocolDiagnostics = (request = {}) => {
  const protocol = requestContextSummary(request).protocol;
  if (!protocol || typeof protocol !== 'object' || Array.isArray(protocol)) return [];
  const inputs = protocolTypes(protocol.input_types, INPUT_PROTOCOL_TYPES);
  const tools = protocolTypes(protocol.request_tool_types, TOOL_PROTOCOL_TYPES);
  const additional = protocolTypes(protocol.additional_tool_types, TOOL_PROTOCOL_TYPES);
  const combined = protocolTypes(protocol.tool_types, TOOL_PROTOCOL_TYPES);
  return [
    `会话压缩项：${protocolCount(protocol.compaction_items)} 项`,
    `本次工具声明：${protocolCount(protocol.request_tools)} 项${tools ? `（${tools}）` : ''}`,
    `历史工具声明（additional_tools）：${protocolCount(protocol.additional_tools)} 项${additional ? `（${additional}）` : ''}`,
    !tools && !additional && combined && `工具类型：${combined}`,
    inputs && `输入项类型：${inputs}`,
  ].filter(Boolean);
};

export const explainUpstreamFailure = (raw, request = {}) => {
  const text = typeof raw === 'string' ? raw : JSON.stringify(raw || {});
  let envelope;
  try { envelope = JSON.parse(text); } catch { envelope = {}; }
  const error = envelope?.error || envelope?.response?.error || envelope;
  const code = String(error?.code || error?.type || '').toLowerCase();
  const message = String(error?.message || text).toLowerCase();
  const identifiers = [error?.code, error?.type].map(value => String(value || '').toLowerCase());
  if (identifiers.some(value => ['insufficient_quota', 'insufficient_user_quota', 'credit_balance_exhausted', 'billing_hard_limit_reached', 'balance_exhausted'].includes(value))
    || code === 'accessdenied' && message.trim().replace(/\.$/, '') === 'current user is in debt') {
    return { code: 'balance_exhausted', summary: '上游账号余额已耗尽',
      advice: '核对上游账号余额并充值，或在调度方案中选择其他凭证分组；单纯释放端点不会补充余额。' };
  }
  if (identifiers.some(value => ['rate_limit_exceeded', 'rate_limit_error', 'too_many_requests', 'rate_limit'].includes(value))) {
    return { code: 'rate_limited', summary: FAILURE_REASON_DETAILS.rate_limited,
      advice: '按上游要求等待后重试，或选择其他凭证分组；同一凭证、同一模型的其他端点仍受此限制。' };
  }
  if (identifiers.some(value => ['permission_denied', 'permission_error', 'insufficient_permissions'].includes(value))) {
    return { code: 'permission_denied', summary: FAILURE_REASON_DETAILS.permission_denied,
      advice: '核对此凭证的模型与接口访问权限，或选择有权限的凭证分组。' };
  }
  if (code === 'invalid_encrypted_content' || /\binvalid_encrypted_content\b/.test(message)) {
    const recovery = getEncryptedHistoryRecovery(request);
    const restored = getCompletedReasoningRecovery(request);
    return { code: 'invalid_encrypted_content', summary: FAILURE_REASON_DETAILS.invalid_encrypted_content,
      advice: recovery
        ? '本次已尝试移除加密推理历史，仍未恢复。请检查上游的会话状态兼容性；压缩历史和工具记录未被清除。'
        : (restored ? '本次已补回观察到的完整推理状态，上游仍未接受。' : '完整状态恢复需要同一会话和来源的完成项记录，且受剩余预算限制。')
          + '可在请求控制中开启“Codex 会话降级恢复”作为最后兜底；它会舍弃隐藏推理，且不处理压缩历史或服务端会话引用。' };
  }
  if (['context_length_exceeded', 'input_too_long'].includes(code)
    || /\b(context_length_exceeded|input_too_long)\b/.test(message)
    || /maximum context length|exceeds? (?:the |this model's )?context (?:window|length)/.test(message)) {
    return { code: 'context_length_exceeded', summary: FAILURE_REASON_DETAILS.context_length_exceeded,
      advice: '请先压缩会话、减少附带文件或工具输出，或切换到支持更长上下文的模型。' };
  }
  if (message.includes('invalid tool type')) {
    const context = requestContextSummary(request);
    const protocol = context.protocol;
    const breakdown = context.parts?.summary?.breakdown;
    const compactionCount = protocol?.compaction_items;
    const compacted = Number.isSafeInteger(compactionCount) && compactionCount >= 0
      ? compactionCount > 0 : Array.isArray(breakdown) && breakdown.some(item => item?.kind === 'compaction');
    const toolTypes = TOOL_PROTOCOL_TYPES.filter(kind => protocolCount(protocol?.request_tool_types?.[kind]) > 0
      || protocolCount(protocol?.additional_tool_types?.[kind]) > 0 || protocolCount(protocol?.tool_types?.[kind]) > 0);
    // A saved channel preference is not proof of the configuration loaded by
    // this request's client. Explain recorded tools without inferring its mode.
    const toolAdvice = toolTypes.length
      ? `请求中的工具声明包含 ${toolTypes.join('、')}；上游未指出具体被拒绝的类型。`
      : '上游未指出具体被拒绝的工具类型。';
    return { code: 'invalid_tool_type', summary: '上游不接受本次请求的工具格式',
      advice: toolAdvice + (compacted
        ? '本次还含原生压缩状态，但该错误不能确认压缩状态是原因。普通摘要不会改变工具格式，已有压缩状态不会自动转换。请确认客户端配置已应用并重启，再用新会话验证。'
        : '请检查上游是否支持当前客户端使用的工具格式。') };
  }
  return null;
};

export const getEncryptedHistoryRecovery = (request = {}) => {
  const counts = getEndpointChainItems(request).map(item => Number(item?.removed_reasoning_items ?? item?.removedReasoningItems ?? 0));
  const removed = counts.filter(count => Number.isSafeInteger(count) && count > 0);
  return removed.length ? Math.max(...removed) : 0;
};

export const getCompletedReasoningRecovery = (request = {}) => {
  const counts = getEndpointChainItems(request).map(item => Number(item?.restored_reasoning_items ?? item?.restoredReasoningItems ?? 0));
  const restored = counts.filter(count => Number.isSafeInteger(count) && count > 0);
  return restored.length ? Math.max(...restored) : 0;
};

export const describeFailureReason = (failureReason) => {
  const code = extractFailureCode(failureReason);
  if (code && FAILURE_REASON_DETAILS[code]) return FAILURE_REASON_DETAILS[code];
  const raw = typeof failureReason === 'string' ? failureReason.trim() : '';
  // 旧格式的中文提示直接沿用
  if (raw.includes('所有渠道均已尝试') || raw.includes('没有可用的故障转移渠道')) return FAILURE_REASON_DETAILS.all_channels_unavailable;
  if (raw.includes('最大循环轮数')) return FAILURE_REASON_DETAILS.max_loops_exceeded;
  if (raw.includes('最大重启次数') || raw.includes('exceeded maximum restart attempts')) return FAILURE_REASON_DETAILS.max_restarts_exceeded;
  return '';
};

export const shouldNotifyFailure = (failureReason) => {
  const code = extractFailureCode(failureReason);
  if (code) return NOTIFY_REASONS.has(code);
  // 旧格式没有分类前缀时退回文本匹配
  return Boolean(describeFailureReason(failureReason));
};

export const failureNeedsManualIntervention = (failureReason) => {
  const code = extractFailureCode(failureReason);
  if (code) return MANUAL_INTERVENTION_REASONS.has(code);
  const raw = typeof failureReason === 'string' ? failureReason : '';
  return raw.includes('最大循环轮数') || raw.includes('最大重启次数');
};

const providerLabel = (provider) => (String(provider || '').toLowerCase() === 'codex' ? 'Codex' : 'Claude');

// 链路里往往跨多个分组、多个端点；提示只点名单个端点会误导，这里按范围汇总。
const describeRoute = (request) => {
  const items = getEndpointChainItems(request);
  const nodes = items.map((item, index) => parseChainNode(item, index + 1));
  const channels = new Set(nodes.map((node) => node.channel).filter(Boolean));
  const groups = new Set(nodes.map((node) => node.credentialGroup).filter(Boolean));
  const endpoints = new Set(nodes.map((node) => node.endpoint).filter(Boolean));
  const channel = request.channel || nodes[0]?.channel || '';

  if (nodes.length <= 1) {
    const group = nodes[0]?.credentialGroup || request.group || request.groupName || request.group_name || '';
    const endpoint = nodes[0]?.endpoint || request.endpoint || '';
    return [channel && `渠道 ${channel}`, group && `分组 ${group}`, endpoint && `端点 ${endpoint}`].filter(Boolean).join(' · ');
  }

  const scope = [];
  if (channels.size > 1) scope.push(`${channels.size} 个渠道`);
  if (groups.size > 0) scope.push(`${groups.size} 个分组`);
  scope.push(`${endpoints.size || nodes.length} 个端点`);
  const prefix = channels.size <= 1 && channel ? `渠道 ${channel}，` : '';
  return `${prefix}已尝试 ${scope.join('、')}，共 ${nodes.length} 次`;
};

/**
 * 把一条失败请求整理成右上角提示需要的 { type, title, message }。
 * message 最多两行，为合并后的请求计数留出一行。
 */
export const buildFailureToast = (request = {}, local = getRequestFailureDetails(request), now = Date.now()) => {
  const failureReason = request.failure_reason || request.failureReason || '';
  const manual = failureNeedsManualIntervention(failureReason);
  const provider = providerLabel(request.provider);
  if (local.cooldownBlocked) {
    const retryAt = new Date(local.nextRecoveryAt);
    const next = retryAt.getTime() > now && `最近一条最早可重试时间 ${retryAt.toLocaleTimeString('zh-CN', { hour12: false })}`;
    return { type: 'warning', title: `${provider} 请求未发送`, message: ['候选端点冷却中', next || '在请求追踪中查看恢复状态'] };
  }

  const blockerLabels = [...new Set(local.blockingCandidates.map(item => item.label))];
  const blocker = blockerLabels.length > 1 ? '候选端点存在多种阻塞，详见请求追踪' : blockerLabels[0];
  if (!local.hasUpstream && (local.localTitle === '请求未发送' || local.localTitle === '本地响应处理失败'
    || (request.http_status_is_upstream ?? request.httpStatusIsUpstream) === false || local.localHops.length > 0)) {
    return { type: 'warning', title: `${provider} ${local.localTitle}`, message: [local.localSummary,
      blocker && !local.localSummary.includes(blocker) && blocker].filter(Boolean) };
  }

  const summary = local.upstreamExplanation?.summary || describeFailureReason(failureReason);
  const route = describeRoute(request);
  const lines = [summary || '服务暂时不可用，请稍后重试', local.upstreamExplanation?.advice || blocker || route].filter(Boolean);

  return {
    type: manual ? 'warning' : 'error',
    title: manual ? `${provider} 请求等待处理` : `${provider} 请求失败`,
    message: lines,
  };
};

const formatCountEntries = (counts, unit) => (
  Object.entries(counts || {})
    .filter(([name, count]) => name && Number(count) > 0)
    .sort((a, b) => Number(b[1]) - Number(a[1]))
    .map(([name, count]) => `${name}：${count} 个${unit}`)
);

/**
 * 挂起 / 等待手动重试的提示：把「几个请求」落到具体端点或渠道上。
 */
export const buildSuspensionToast = ({ suspendedCount = 0, endpointCounts = {}, manualRetryCount = 0, channelCounts = {} } = {}) => {
  if (manualRetryCount > 0) {
    const lines = [`${manualRetryCount} 个请求已达最大循环轮数，等待手动重试`];
    lines.push(...formatCountEntries(channelCounts, '请求').map((line) => `渠道 ${line}`));
    lines.push('请在渠道页重新激活渠道，或点击重试');
    return { type: 'warning', title: 'Claude 请求等待重试', message: lines };
  }
  if (suspendedCount > 0) {
    const lines = [`${suspendedCount} 个请求已挂起，等待端点恢复或渠道切换`];
    lines.push(...formatCountEntries(endpointCounts, '请求').map((line) => `端点 ${line}`));
    return { type: 'info', title: 'Claude 请求已挂起', message: lines };
  }
  return null;
};
