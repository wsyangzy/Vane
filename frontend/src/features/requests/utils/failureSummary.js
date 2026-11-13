// ============================================
// 失败摘要 - 请求失败的统一文案构造
// 供右上角全局提示与请求详情弹窗共用，保证两处说法一致。
// 本模块只依赖同目录的 endpointChain，可直接被 node --test 加载。
// ============================================

import { getEndpointChainItems, parseChainNode } from './endpointChain.js';

// failure_reason 是 Relay 的内部分类；这里给每个分类一句用户能看懂的说明。
// last_failure_reason may contain provider text or a local failure; provenance
// and endpoint-chain evidence decide which card owns it.
export const FAILURE_REASON_DETAILS = Object.freeze({
  no_healthy_endpoints: '当前渠道没有可用端点，请求未转发到提供商',
  no_supported_model_endpoint: '当前渠道没有支持该模型的端点，请求未转发到提供商',
  daily_cost_limit_exceeded: '凭证分组已达每日成本限额，请求未转发到提供商',
  daily_cost_state_unavailable: '暂时无法读取每日成本，请求未转发到提供商',
  retry_budget_exhausted: 'Vane 已达到本次请求重试上限并停止继续尝试',
  suspension_timeout: '挂起等待端点恢复超时',
  all_channels_unavailable: '所有渠道均已尝试，没有可用的故障转移渠道',
  max_loops_exceeded: '已达最大循环轮数，等待手动重试',
  max_restarts_exceeded: '已达最大重启次数，请求终止',
  endpoint_exhausted: '当前渠道端点已全部尝试失败',
  route_exhausted: '模型路由规则的全部目标均已尝试失败',
  non_stream_route_exhausted: '非流改道的全部目标均已尝试失败',
  stream_truncated: '上游流式响应中途截断',
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

// A provider reply and a local scheduling/transport outcome have different
// evidence. Never render an internal classification as the provider's words.
export const getRequestFailureDetails = (request = {}) => {
  const nodes = getEndpointChainItems(request).map((item, index) => parseChainNode(item, index + 1));
  const localHops = nodes.filter(node => node.local || node.skipReason).reverse();
  const status = Number(request.http_status_code ?? request.httpStatusCode ?? request.status_code ?? request.httpStatus ?? 0) || 0;
  const source = request.http_status_is_upstream ?? request.httpStatusIsUpstream;
  const reason = request.failure_reason || request.failureReason || '';
  const processingFailure = ['response_read_error', 'response_write_error', 'privacy_filter_failed'].includes(reason);
  const upstreamHops = nodes.filter(node => !node.local && node.statusCode > 0 && (!processingFailure || node.statusCode >= 400)).reverse();
  const hasUpstream = upstreamHops.length > 0 || source !== false && status > 0 && (!processingFailure || status >= 400);
  const raw = String(request.last_failure_reason || request.lastFailureReason || '');
  const detail = raw !== reason ? raw : '';
  const skipReason = localHops.find(node => node.skipReason)?.skipReason || '';
  const admissionReason = /^(account_|subscription_|state_|profile_|routing_|candidate_)/.test(reason)
    || reason.startsWith('search_') || reason === 'daily_cost_state_unavailable'
    || ['no_healthy_endpoints', 'no_supported_model_endpoint', 'daily_cost_limit_exceeded', 'privacy_filter_failed', 'image_streaming_unsupported'].includes(reason);
  const sent = nodes.some(node => !node.skipReason && (node.attempts > 0 || node.statusCode > 0 || node.error));
  const unsent = !hasUpstream && !sent && (Boolean(skipReason) || request.dispatchTrace?.attemptCount === 0 || admissionReason);
  return {
    upstreamHops, localHops, hasUpstream,
    upstreamStatus: hasUpstream ? status : 0,
    upstreamDetail: hasUpstream ? processingFailure ? upstreamHops[0]?.error || '' : detail : '',
    showLocal: processingFailure || localHops.length > 0 || !hasUpstream,
    localTitle: unsent ? '请求未发送' : processingFailure ? '本地响应处理失败'
      : hasUpstream && localHops.every(hop => hop.skipReason) ? '部分目标未发送' : '未收到上游响应',
    localSummary: ['endpoint_exhausted', 'route_exhausted', 'non_stream_route_exhausted', 'all_channels_unavailable'].includes(reason) && !skipReason
      ? localHops.some(hop => hop.error) || detail ? '连接或响应传输未完成' : '未记录具体的本地失败原因'
      : describeFailureReason(skipReason || reason) || (unsent ? '本地调度未选出可发送的目标' : '本次请求没有可用的上游响应记录'),
    localDetail: processingFailure || !hasUpstream ? detail : '',
    category: reason === 'auxiliary_endpoint_unsupported' ? '辅助接口不受支持' : reason === 'image_endpoint_unsupported' ? '图像接口不受支持' : '',
  };
};

// 需要用户介入（激活渠道、点重试）的分类用 warning，其余终态失败用 error。
const MANUAL_INTERVENTION_REASONS = new Set(['max_loops_exceeded', 'max_restarts_exceeded']);

// 只对「整条链路都失败」或「Relay 本地拒绝」的终态弹提示；
// 单端点失败会被故障转移接住，弹出来只会淹没真正需要处理的提示。
const NOTIFY_REASONS = new Set([
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
  'suspension_timeout',
  'account_cooling_down',
  'account_disabled',
  'account_missing',
  'account_repair_required',
  'auxiliary_endpoint_unsupported',
]);

// 旧记录的 failure_reason 形如 "endpoint_exhausted: request failed: ..."，冒号前才是分类。
export const extractFailureCode = (failureReason) => {
  if (typeof failureReason !== 'string') return '';
  const head = failureReason.trim().split(':')[0].trim().toLowerCase();
  return /^[a-z][a-z0-9_]*$/.test(head) ? head : '';
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
 * message 是行数组，Toast 会渲染成列表：第一行是原因，第二行是尝试范围。
 * 上游报错原文、请求 ID、模型这类排查细节留给请求追踪页。
 */
export const buildFailureToast = (request = {}) => {
  const failureReason = request.failure_reason || request.failureReason || '';
  const manual = failureNeedsManualIntervention(failureReason);
  const provider = providerLabel(request.provider);
  const local = getRequestFailureDetails(request);
  if ((request.http_status_is_upstream === false || request.httpStatusIsUpstream === false || local.localHops.length > 0 || local.localTitle === '本地响应处理失败') && !local.hasUpstream) {
    return { type: 'warning', title: `${provider} ${local.localTitle}`, message: [local.localSummary] };
  }

  const lines = [];
  const summary = describeFailureReason(failureReason);
  if (summary) lines.push(summary);
  const route = describeRoute(request);
  if (route) lines.push(route);

  return {
    type: manual ? 'warning' : 'error',
    title: manual ? `${provider} 请求等待处理` : `${provider} 请求失败`,
    message: lines.length > 0 ? lines : ['服务暂时不可用，请稍后重试'],
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
