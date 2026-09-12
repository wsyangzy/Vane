// ============================================
// 失败摘要 - 请求失败的统一文案构造
// 供右上角全局提示与请求详情弹窗共用，保证两处说法一致。
// 本模块只依赖同目录的 endpointChain，可直接被 node --test 加载。
// ============================================

import { getEndpointChainItems, parseChainNode } from './endpointChain.js';

// failure_reason 是 Relay 的内部分类；这里给每个分类一句用户能看懂的说明。
// last_failure_reason 才是上游原文，展示时以原文优先，分类说明只做兜底。
export const FAILURE_REASON_DETAILS = Object.freeze({
  no_healthy_endpoints: '当前渠道没有可用端点，请求未转发到提供商',
  no_supported_model_endpoint: '当前渠道没有支持该模型的端点，请求未转发到提供商',
  daily_cost_limit_exceeded: '凭证分组已达每日成本限额，请求未转发到提供商',
  retry_budget_exhausted: '全局重试预算已耗尽',
  suspension_timeout: '挂起等待端点恢复超时',
  all_channels_unavailable: '所有渠道均已尝试，没有可用的故障转移渠道',
  max_loops_exceeded: '已达最大循环轮数，等待手动重试',
  max_restarts_exceeded: '已达最大重启次数，请求终止',
  endpoint_exhausted: '当前渠道端点已全部尝试失败',
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
});

// 需要用户介入（激活渠道、点重试）的分类用 warning，其余终态失败用 error。
const MANUAL_INTERVENTION_REASONS = new Set(['max_loops_exceeded', 'max_restarts_exceeded']);

// 只对「整条链路都失败」或「Relay 本地拒绝」的终态弹提示；
// 单端点失败会被故障转移接住，弹出来只会淹没真正需要处理的提示。
const NOTIFY_REASONS = new Set([
  'all_channels_unavailable',
  'endpoint_exhausted',
  'max_loops_exceeded',
  'max_restarts_exceeded',
  'no_healthy_endpoints',
  'no_supported_model_endpoint',
  'daily_cost_limit_exceeded',
  'retry_budget_exhausted',
  'suspension_timeout',
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
