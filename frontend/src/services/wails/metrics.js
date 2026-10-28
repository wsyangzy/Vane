import { WailsApp, initWails } from './connection.js';

// ============================================
// 使用统计 API
// ============================================

/**
 * 获取使用统计（与 HTTP API 格式一致）
 * @param {Object} params - 查询参数
 * @param {string} params.period - 时间周期: "1h", "1d", "7d", "30d", "90d"
 * @param {string} params.start_date - 开始时间（优先于 period）
 * @param {string} params.end_date - 结束时间（优先于 period）
 * @param {string} params.status - 状态筛选
 * @param {string} params.model - 模型筛选
 * @param {string} params.endpoint - 端点筛选
 * @param {string} params.group - 组筛选
 * @returns {Promise<Object>} - 统计数据
 */
const getUsageStats = async (params = {}) => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');

  // 构建查询参数对象
  // 注意：前端 buildQueryParams() 返回 start_date/end_date（带下划线）
  const queryParams = {
    period: params.period || '30d',
    start_date: params.start_date || '',
    end_date: params.end_date || '',
    status: params.status || '',
    model: params.model || '',
    channel: params.channel || '',
    endpoint: params.endpoint || '',
    group: params.group || '',
    provider: params.provider || ''
  };

  console.log('📊 [Wails] GetUsageStats 参数:', queryParams);
  const data = await WailsApp.GetUsageStats(queryParams);

  // 返回与 HTTP API 一致的格式
  return {
    period: data.period || queryParams.period,
    total_requests: data.total_requests || 0,
    success_rate: data.success_rate || 0,
    avg_duration_ms: data.avg_duration_ms || 0,
    total_cost_usd: data.total_cost_usd || 0,
    total_tokens: data.total_tokens || 0,
    failed_requests: data.failed_requests || 0,
    // null 表示没有输入用量，展示为 "-"；0 是真实的 0%
    cache_hit_rate: normalizeRate(data.cache_hit_rate)
  };
};

// 比率字段：后端用 null 表示无数据，不能用 || 0 抹成 0%
const normalizeRate = (value) => {
  if (value === null || value === undefined || value === '') return null;
  const rate = Number(value);
  return Number.isFinite(rate) ? rate : null;
};

const getUsageSummary = async (startTime = '', endTime = '', provider = '') => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');

  const summary = await WailsApp.GetUsageSummary(startTime, endTime, provider);

  return {
    total_requests: summary.total_requests || 0,
    all_time_total_requests: summary.all_time_total_requests || 0, // 全部历史请求数
    today_requests: summary.today_requests || 0,            // 今日请求数
    successful_requests: summary.success_requests || 0,
    failed_requests: summary.failed_requests || 0,
    total_input_tokens: summary.total_input_tokens || 0,
    total_output_tokens: summary.total_output_tokens || 0,
    total_cost: summary.total_cost || 0,
    today_cost: summary.today_cost || 0,                    // 今日成本
    all_time_total_cost: summary.all_time_total_cost || 0,  // 全部历史成本
    today_tokens: summary.today_tokens || 0,                // 今日 tokens
    all_time_total_tokens: summary.all_time_total_tokens || 0,  // 全部历史 tokens
    total_tokens: summary.total_tokens || ((summary.total_input_tokens || 0) + (summary.total_output_tokens || 0))
  };
};

const mapRequestNetworkFields = (record = {}) => {
  const httpStatusIsUpstream = Boolean(record.http_status_is_upstream ?? record.httpStatusIsUpstream ?? false);
  const httpStatus = httpStatusIsUpstream
    ? (record.http_status ?? record.httpStatus ?? record.http_status_code ?? record.httpStatusCode ?? 0)
    : 0;
  return {
    status_code: httpStatus,
    http_status_code: httpStatus,
    http_status_is_upstream: httpStatusIsUpstream,
    httpStatusIsUpstream,
    retry_count: record.retry_count ?? record.retryCount ?? 0,
    method: record.method || '',
    path: record.path || '',
    client_ip: record.client_ip || record.clientIP || '',
    user_agent: record.user_agent || record.userAgent || '',
  };
};

const getRequests = async (params = {}) => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');

  // 构建 Wails 绑定参数对象
  // 注意：前端 buildQueryParams() 返回 start_date/end_date（带下划线）
  const queryParams = {
    page: parseInt(params.page || 1),
    page_size: parseInt(params.limit || params.pageSize || 50),
    start_date: params.start_date || params.start_time || params.startDate || '',
    end_date: params.end_date || params.end_time || params.endDate || '',
    status: params.status || '',
    model: params.model || '',
    channel: params.channel || '',
    endpoint: params.endpoint || '',
    group: params.group || '',
    provider: params.provider || ''
  };

  console.log('🔍 [Wails] GetRequests 参数:', queryParams);
  const result = await WailsApp.GetRequests(queryParams);
  console.log('🔍 [Wails] GetRequests 返回:', result);

  // 转换请求记录格式
  const requests = (result.requests || []).map(r => ({
    request_id: r.request_id,
    id: r.request_id,
    timestamp: r.timestamp,
    start_time: r.timestamp,
    channel: r.channel || '',
    endpoint_name: r.endpoint,
    endpoint: r.endpoint,
    endpoint_chain: r.endpoint_chain || r.endpointChain || '',
    group_name: r.group,
    group: r.group,
    model_name: r.model,
    model: r.model,
    original_model_name: r.original_model_name || '',
    model_rewrite_source: r.model_rewrite_source || '',
    model_rewrite_target: r.model_rewrite_target || '',
    originalModelName: r.original_model_name || '',
    modelRewriteSource: r.model_rewrite_source || '',
    modelRewriteTarget: r.model_rewrite_target || '',
    route_snapshot: r.route_snapshot || '',
    routeSnapshot: r.route_snapshot || '',
    reasoning_effort: r.reasoning_effort || '',
    reasoningEffort: r.reasoning_effort || '',
    service_tier: r.service_tier || '',
    serviceTier: r.service_tier || '',
    privacy_redacted: Boolean(r.privacy_redacted),
    privacy_redaction_summary: r.privacy_redaction_summary || '',
    privacyRedacted: Boolean(r.privacy_redacted),
    privacyRedactionSummary: r.privacy_redaction_summary || '',
    provider: r.provider || '',
    status: r.status,
    // 终态枚举；历史行与进行中的请求为空
    stream_outcome: r.stream_outcome || '',
    streamOutcome: r.stream_outcome || '',
    ...mapRequestNetworkFields(r),
    input_tokens: r.input_tokens,
    output_tokens: r.output_tokens,
    reasoning_tokens: r.reasoning_tokens ?? 0,
    billable_output_tokens: r.billable_output_tokens ?? r.output_tokens ?? 0,
    billing_output_token_source: r.billing_output_token_source || 'output_tokens',
	 billing_rule_snapshot: r.billing_rule_snapshot || '',
    cache_creation_tokens: r.cache_creation_tokens,
    cache_creation_5m_tokens: r.cache_creation_5m_tokens,
    cache_creation_1h_tokens: r.cache_creation_1h_tokens,
    cache_read_tokens: r.cache_read_tokens,
    duration_ms: r.response_time,
    duration: r.response_time,
    first_byte_ms: r.first_byte_ms ?? r.firstByteMs ?? null,
    is_streaming: r.is_streaming,
    total_cost_usd: r.cost,
    cost: r.cost,
    // 成本明细字段（计费详情展示用）
    input_cost_usd: r.input_cost_usd || 0,
    output_cost_usd: r.output_cost_usd || 0,
    cache_creation_cost_usd: r.cache_creation_cost_usd || 0,
    cache_creation_5m_cost_usd: r.cache_creation_5m_cost_usd || 0,
    cache_creation_1h_cost_usd: r.cache_creation_1h_cost_usd || 0,
    cache_read_cost_usd: r.cache_read_cost_usd || 0,
    // 端点倍率快照（计费详情展示用）
    cost_multiplier: r.cost_multiplier ?? 1.0,
    input_cost_multiplier: r.input_cost_multiplier ?? 1.0,
    output_cost_multiplier: r.output_cost_multiplier ?? 1.0,
    cache_creation_cost_multiplier: r.cache_creation_cost_multiplier ?? 1.0,
    cache_creation_cost_multiplier_1h: r.cache_creation_cost_multiplier_1h ?? 1.0,
    cache_read_cost_multiplier: r.cache_read_cost_multiplier ?? 1.0,
    model_cost_multiplier: r.model_cost_multiplier ?? 1.0,
    model_input_cost_multiplier: r.model_input_cost_multiplier ?? 1.0,
    model_output_cost_multiplier: r.model_output_cost_multiplier ?? 1.0,
    model_cache_read_cost_multiplier: r.model_cache_read_cost_multiplier ?? 1.0,
    // 错误信息字段
    failure_reason: r.failure_reason || '',
    last_failure_reason: r.last_failure_reason || '',
    lastFailureReason: r.last_failure_reason || '',
    cancel_reason: r.cancel_reason || ''
  }));

  return {
    requests,
    total: result.total,
    page: result.page,
    pageSize: result.page_size,
    totalPages: Math.ceil(result.total / result.page_size)
  };
};

// ============================================
// 图表数据 API
// ============================================

/**
 * 获取请求趋势图表数据
 * @param {number} minutes - 时间范围（分钟）
 * @param {string} provider - 提供商过滤（'claude'/'codex'/'')
 * @returns {Promise<Array>} - 图表数据点数组 [{time, total, success, fail}]
 */
const getRequestTrendChart = async (minutes = 30, provider = '') => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');

  console.log('📊 [Wails] 调用 GetRequestTrendChart, minutes:', minutes, 'provider:', provider);
  const data = await WailsApp.GetRequestTrendChart(minutes, provider);
  console.log('📊 [Wails] GetRequestTrendChart 返回:', data ? `${data.length} 个数据点` : '无数据', data);
  return data || [];
};

/**
 * 获取响应时间图表数据
 * @param {number} minutes - 时间范围（分钟）
 * @param {string} provider - 提供商过滤（'claude'/'codex'/'')
 * @returns {Promise<Array>} - 图表数据点数组 [{time, avg, min, max}]
 */
const getResponseTimeChart = async (minutes = 30, provider = '') => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');

  const data = await WailsApp.GetResponseTimeChart(minutes, provider);
  return data || [];
};

/**
 * 获取连接活动图表数据
 * @param {number} minutes - 时间范围（分钟）
 * @param {string} provider - 提供商过滤（'claude'/'codex'/'')
 * @returns {Promise<Array>} - 图表数据点数组 [{time, value}]
 */
const getConnectionActivityChart = async (minutes = 60, provider = '') => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');

  const data = await WailsApp.GetConnectionActivityChart(minutes, provider);
  // 转换为前端期望的格式 (connections 字段)
  return (data || []).map(point => ({
    time: point.time,
    timestamp: point.timestamp,
    connections: point.value
  }));
};

// ============================================
// Token 使用统计 API
// ============================================

/**
 * 获取 Token 使用统计（今日 request_logs 数据）
 * @param {string} provider - 提供商过滤（'claude'/'codex'/'')
 * @returns {Promise<Object>} - Token 使用数据 {input, output, cacheCreation, cacheRead}
 */
const getTokenUsage = async (provider = '') => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');

  const data = await WailsApp.GetTokenUsage(provider);
  return {
    input: data.input_tokens || 0,
    output: data.output_tokens || 0,
    cacheCreation: data.cache_creation_tokens || 0,
    cacheRead: data.cache_read_tokens || 0,
    total: data.total_tokens || 0
  };
};

const getTokenUsageHeatmap = async (params = {}) => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');

  const queryParams = {
    page: 0,
    page_size: 0,
    start_date: '',
    end_date: '',
    status: '',
    model: params.model || '',
    channel: '',
    endpoint: '',
    group: '',
    provider: params.provider || ''
  };

  const items = await WailsApp.GetTokenUsageHeatmap(queryParams);
  return (items || []).map(item => ({
    date: item.date || '',
    bucket: item.bucket || 'day',
    requestCount: item.request_count || 0,
    input: item.input_tokens || 0,
    output: item.output_tokens || 0,
    cacheCreation: item.cache_creation_tokens || 0,
    cacheRead: item.cache_read_tokens || 0,
    total: item.total_tokens || 0,
    intensity: Number(item.intensity || 0)
  }));
};

// ============================================
// 端点健康状态图表 API
// ============================================

/**
 * 获取端点健康状态数据
 * @returns {Promise<Object>} - 健康状态数据 {healthy, unhealthy, skipped, excluded, total}
 */
const getEndpointHealthChart = async () => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');

  const data = await WailsApp.GetEndpointHealthChart();
  return {
    healthy: data.healthy || 0,
    unhealthy: data.unhealthy || 0,
    skipped: data.skipped || 0,
    excluded: data.excluded || 0,
    total: data.total || 0
  };
};

/**
 * 获取端点最近 N 小时的可用性时间条数据
 * 数据来自真实请求记录；没有流量的小时以 hasData=false 的空档返回，不能省略
 * @returns {Promise<Object>} {slices, hasData, total, success, failed, availability, avgFirstByteMs}
 */
const getEndpointAvailability = async ({
  provider = '',
  channel = '',
  endpointName = '',
  hours = 24
} = {}) => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');

  const data = await WailsApp.GetEndpointAvailability(provider, channel, endpointName, hours);
  const slices = Array.isArray(data?.slices) ? data.slices : [];
  return {
    slices: slices.map((slice) => ({
      key: slice.key || '',
      label: slice.label || '',
      hasData: Boolean(slice.has_data ?? slice.hasData),
      total: slice.total || 0,
      success: slice.success || 0,
      failed: slice.failed || 0,
      availability: slice.availability || 0,
      avgFirstByteMs: slice.avg_first_byte_ms ?? slice.avgFirstByteMs ?? 0
    })),
    hasData: Boolean(data?.has_data ?? data?.hasData),
    total: data?.total || 0,
    success: data?.success || 0,
    failed: data?.failed || 0,
    availability: data?.availability || 0,
    avgFirstByteMs: data?.avg_first_byte_ms ?? data?.avgFirstByteMs ?? 0
  };
};

/**
 * 获取 Codex 端点健康状态数据（仅当前激活渠道下的端点）
 * @returns {Promise<Object>} - 健康状态数据 {healthy, unhealthy, skipped, excluded, total}
 */
const getCodexEndpointHealthChart = async () => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');

  const data = await WailsApp.GetCodexEndpointHealthChart();
  return {
    healthy: data.healthy || 0,
    unhealthy: data.unhealthy || 0,
    skipped: data.skipped || 0,
    excluded: data.excluded || 0,
    total: data.total || 0
  };
};

// ============================================
// 端点成本图表 API
// ============================================

/**
 * 获取当日端点成本数据
 * @param {string} provider - 提供商过滤（'claude'/'codex'/'')
 * @returns {Promise<Array>} - 端点成本数据 [{name, channel, endpoint, group, tokens, cost, provider}]
 */
const getEndpointCosts = async (provider = '') => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');

  const data = await WailsApp.GetEndpointCosts(provider);
  // 数据已经是 [{name, channel, endpoint, group, tokens, cost, provider}] 格式
  return data || [];
};

export { getUsageStats, normalizeRate, getUsageSummary, mapRequestNetworkFields, getRequests, getRequestTrendChart, getResponseTimeChart, getConnectionActivityChart, getTokenUsage, getTokenUsageHeatmap, getEndpointHealthChart, getEndpointAvailability, getCodexEndpointHealthChart, getEndpointCosts };
