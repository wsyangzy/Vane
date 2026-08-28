// ============================================
// API 服务层
// 2025-11-28 (Updated 2025-12-04 for Wails support)
// ============================================

import { API_ENDPOINTS, ERROR_MESSAGES } from './constants.js';
import * as WailsApi from './wailsApi.js';

// 检测是否在 Wails 环境中运行
export const isWailsEnvironment = WailsApi.isWailsEnvironment;

// 请求超时配置
const DEFAULT_TIMEOUT = 30000;

const parseJSONObject = (value) => {
  if (value && typeof value === 'object' && !Array.isArray(value)) return value;
  if (typeof value !== 'string' || !value.trim()) return null;
  try {
    const parsed = JSON.parse(value);
    return parsed && typeof parsed === 'object' && !Array.isArray(parsed) ? parsed : null;
  } catch {
    return null;
  }
};

// 带超时的 fetch 包装器
const fetchWithTimeout = async (url, options = {}, timeout = DEFAULT_TIMEOUT) => {
  const controller = new AbortController();
  const timeoutId = setTimeout(() => controller.abort(), timeout);

  try {
    const response = await fetch(url, {
      ...options,
      signal: controller.signal,
      headers: {
        'Content-Type': 'application/json',
        ...options.headers
      }
    });

    clearTimeout(timeoutId);

    if (!response.ok) {
      let errorMessage = ERROR_MESSAGES.SERVER_ERROR;
      try {
        const errorData = await response.json();
        errorMessage = errorData.message || errorData.error || errorMessage;
      } catch {
        errorMessage = `HTTP ${response.status}: ${response.statusText}`;
      }
      throw new Error(errorMessage);
    }

    const contentType = response.headers.get('content-type');
    if (contentType && contentType.includes('application/json')) {
      return await response.json();
    }
    return await response.text();
  } catch (error) {
    clearTimeout(timeoutId);
    if (error.name === 'AbortError') {
      throw new Error(ERROR_MESSAGES.REQUEST_TIMEOUT);
    }
    if (error instanceof TypeError && error.message.includes('fetch')) {
      throw new Error(ERROR_MESSAGES.NETWORK_ERROR);
    }
    throw error;
  }
};

// ============================================
// 系统状态 API
// ============================================

export const fetchStatus = async () => {
  // Wails 环境使用绑定
  if (isWailsEnvironment()) {
    return await WailsApi.getSystemStatus();
  }
  return await fetchWithTimeout(API_ENDPOINTS.STATUS);
};

export const fetchConnections = async () => {
  // Wails 环境使用绑定
  if (isWailsEnvironment()) {
    return await WailsApi.getUsageSummary();
  }
  return await fetchWithTimeout(API_ENDPOINTS.CONNECTIONS);
};

// ============================================
// 端点管理 API
// ============================================

export const fetchEndpoints = async () => {
  // Wails 环境使用绑定
  if (isWailsEnvironment()) {
    return await WailsApi.getEndpoints();
  }

  const data = await fetchWithTimeout(API_ENDPOINTS.ENDPOINTS);
  // 返回完整结构，包含 total, healthy 等统计信息
  // API 返回格式: { endpoints: [...], total: N, healthy: N }
  if (data.endpoints) {
    return {
      endpoints: data.endpoints,
      total: data.total ?? data.endpoints.length,
      healthy: data.healthy ?? data.endpoints.filter(e => e.status === 'healthy').length
    };
  }
  // 如果 API 直接返回数组
  const endpoints = Array.isArray(data) ? data : [];
  return {
    endpoints,
    total: endpoints.length,
    healthy: endpoints.filter(e => e.status === 'healthy').length
  };
};

export const checkEndpointHealth = async (endpointName) => {
  // Wails 环境使用绑定
  if (isWailsEnvironment()) {
    return await WailsApi.triggerHealthCheck(endpointName);
  }
  return await fetchWithTimeout(`${API_ENDPOINTS.ENDPOINT_HEALTH}/${endpointName}`, {
    method: 'POST'
  });
};

export const checkAllEndpointsHealth = async () => {
  // Wails 环境：使用后端批量健康检查 API
  if (isWailsEnvironment()) {
    const result = await WailsApi.batchHealthCheckAll();
    return {
      success: result.success,
      message: result.message || '批量健康检测完成',
      total: result.total,
      healthy_count: result.healthy_count,
      unhealthy_count: result.unhealthy_count
    };
  }
  return await fetchWithTimeout(API_ENDPOINTS.ENDPOINT_HEALTH, {
    method: 'POST'
  });
};

/**
 * 更新端点优先级
 * @param {string} endpointName - 端点名称
 * @param {number} priority - 新优先级
 */
export const updateEndpointPriority = async (endpointName, priority) => {
  if (!endpointName) throw new Error('端点名称不能为空');
  if (priority < 1) throw new Error('优先级必须大于等于1');

  // Wails 环境使用绑定
  if (isWailsEnvironment()) {
    await WailsApi.setEndpointPriority(endpointName, parseInt(priority));
    return { success: true, message: `端点 ${endpointName} 优先级已更新为 ${priority}` };
  }

  const data = await fetchWithTimeout(`/api/v1/endpoints/${encodeURIComponent(endpointName)}/priority`, {
    method: 'POST',
    body: JSON.stringify({ priority: parseInt(priority) })
  });

  if (!data.success) {
    throw new Error(data.error || '优先级更新失败');
  }

  return data;
};

/**
 * 获取 Keys 概览数据
 * 返回每个端点的 tokens 列表，用于级联选择器
 */
export const fetchKeysOverview = async () => {
  // Wails 环境使用绑定
  if (isWailsEnvironment()) {
    return await WailsApi.getKeysOverview();
  }
  const data = await fetchWithTimeout(API_ENDPOINTS.KEYS_OVERVIEW);
  // API 返回格式: { endpoints: [{ endpoint: "name", tokens: [...] }, ...] }
  return data;
};

/**
 * 切换端点的 Token
 * @param {string} endpointName - 端点名称
 * @param {string} keyType - 'token' 或 'api_key'
 * @param {number} index - Token 索引
 */
export const switchKey = async (endpointName, keyType, index) => {
  if (!endpointName) throw new Error('端点名称不能为空');
  if (keyType !== 'token' && keyType !== 'api_key') throw new Error('无效的 Key 类型');
  if (typeof index !== 'number' || index < 0) throw new Error('无效的 Key 索引');

  // Wails 环境使用绑定
  if (isWailsEnvironment()) {
    return await WailsApi.switchKey(endpointName, keyType, index);
  }

  const apiPath = keyType === 'token'
    ? `/api/v1/endpoints/${encodeURIComponent(endpointName)}/keys/token`
    : `/api/v1/endpoints/${encodeURIComponent(endpointName)}/keys/api-key`;

  const data = await fetchWithTimeout(apiPath, {
    method: 'POST',
    body: JSON.stringify({ index })
  });

  if (!data.success) {
    throw new Error(data.error || 'Key 切换失败');
  }

  return data;
};

// ============================================
// 组管理 API
// ============================================

export const fetchGroups = async () => {
  // Wails 环境使用绑定
  if (isWailsEnvironment()) {
    return await WailsApi.getGroups();
  }

  const data = await fetchWithTimeout(API_ENDPOINTS.GROUPS);
  // 返回完整结构，包含 groups 数组和 active_group 信息
  // API 返回格式: { groups: [...], active_group: "xxx" } 或直接数组
  const groups = data.groups || (Array.isArray(data) ? data : []);
  const activeGroup = groups.find(g => g.is_active);
  return {
    groups,
    active_group: activeGroup?.name || data.active_group || null,
    total_suspended_requests: data.total_suspended_requests || 0
  };
};

export const activateGroup = async (groupName) => {
  // Wails 环境使用绑定
  if (isWailsEnvironment()) {
    return await WailsApi.activateGroup(groupName);
  }
  const url = API_ENDPOINTS.GROUP_ACTIVATE.replace('{name}', groupName);
  return await fetchWithTimeout(url, { method: 'POST' });
};

export const pauseGroup = async (groupName) => {
  // Wails 环境使用绑定
  if (isWailsEnvironment()) {
    return await WailsApi.pauseGroup(groupName);
  }
  const url = API_ENDPOINTS.GROUP_PAUSE.replace('{name}', groupName);
  return await fetchWithTimeout(url, { method: 'POST' });
};

// 检查是否启用渠道间故障转移
// 用于前端判断是否显示渠道切换器
export const fetchInterChannelFailoverEnabled = async () => {
  // Wails 环境使用绑定
  if (isWailsEnvironment()) {
    return await WailsApi.isInterChannelFailoverEnabled();
  }
  // HTTP 模式暂不支持（桌面应用专用功能）
  return false;
};

// ============================================
// 使用统计 API
// ============================================

export const fetchUsageStats = async (params = {}) => {
  // Wails 环境使用新的 GetUsageStats 绑定（与 HTTP API 格式一致）
  // 传递完整筛选参数
  if (isWailsEnvironment()) {
    return await WailsApi.getUsageStats(params);
  }

  const queryParams = new URLSearchParams();
  Object.entries(params).forEach(([key, value]) => {
    if (value !== null && value !== undefined && value !== '') {
      queryParams.append(key, value.toString());
    }
  });

  const url = queryParams.toString()
    ? `${API_ENDPOINTS.USAGE_STATS}?${queryParams.toString()}`
    : API_ENDPOINTS.USAGE_STATS;

  return await fetchWithTimeout(url);
};

export const fetchRequests = async (params = {}) => {
  // 标准化请求数据（提取到外部，Wails和HTTP环境共用）
  const normalizeRequest = (request) => {
    const normalizeNumberOrNull = (value) => {
      if (value === null || value === undefined || value === '') return null;
      const num = Number(value);
      return Number.isFinite(num) ? num : null;
    };
    const rawRequestId = request.request_id || request.requestId || request.id;
    const rawRequestIdStr = typeof rawRequestId === 'string' ? rawRequestId : String(rawRequestId || '');

    const partialMarker = '-partial-';
    const markerIdx = rawRequestIdStr.indexOf(partialMarker);
    const baseRequestId = markerIdx > 0 ? rawRequestIdStr.slice(0, markerIdx) : rawRequestIdStr;

    // Normalize endpoint chain: handle object array, string array, or legacy string
    const rawChain = request.endpoint_chain || request.endpointChain || '';
    let endpointChain;
    if (Array.isArray(rawChain)) {
      endpointChain = rawChain;
    } else if (typeof rawChain === 'string' && rawChain) {
      try {
        const parsed = JSON.parse(rawChain);
        endpointChain = Array.isArray(parsed) ? parsed : [];
      } catch {
        endpointChain = rawChain.split(' -> ').filter(Boolean);
      }
    } else {
      endpointChain = [];
    }
    const endpoint = request.endpoint_name || request.endpoint || '';

    // Convert chain item (object or string) to "channel::group::endpoint" key string
    const chainItemToKey = (item) => {
      if (typeof item === 'object' && item !== null) {
        const group = item.credential_group || item.credentialGroup || item.group_name || item.group || '';
        return item.channel
          ? `${item.channel}::${group ? `${group}::` : ''}${item.endpoint || ''}`
          : (item.endpoint || '');
      }
      return String(item || '');
    };
    const endpointChainLength = endpointChain.length > 0 ? endpointChain.length : (endpoint ? 1 : 0);
    const endpointChainHead = endpointChain.length > 0 ? chainItemToKey(endpointChain[0]) : (endpoint || '');
    const endpointChainTail = endpointChain.length > 0 ? chainItemToKey(endpointChain[endpointChain.length - 1]) : (endpoint || '');
    const httpStatusCode = normalizeNumberOrNull(
      request.http_status_code ?? request.httpStatusCode ?? request.http_status ?? request.httpStatus ?? request.status_code ?? request.statusCode
    );
    const rawRetryCount = Number(request.retry_count ?? request.retryCount ?? 0);
    const retryCount = Number.isFinite(rawRetryCount) ? Math.max(0, Math.trunc(rawRetryCount)) : 0;
    const clientIP = request.client_ip || request.clientIP || '';
    const userAgent = request.user_agent || request.userAgent || '';

    return ({
      ...request,
      requestId: baseRequestId,
      rawRequestId: rawRequestIdStr,
      isPartialRecord: markerIdx > 0,
      id: rawRequestIdStr,
      timestamp: request.start_time || request.timestamp,
      model: request.model_name || request.model || 'unknown',
      original_model_name: request.original_model_name || request.originalModelName || '',
      model_rewrite_source: request.model_rewrite_source || request.modelRewriteSource || '',
      model_rewrite_target: request.model_rewrite_target || request.modelRewriteTarget || '',
      originalModelName: request.original_model_name || request.originalModelName || '',
      modelRewriteSource: request.model_rewrite_source || request.modelRewriteSource || '',
      modelRewriteTarget: request.model_rewrite_target || request.modelRewriteTarget || '',
      privacy_redacted: Boolean(request.privacy_redacted ?? request.privacyRedacted),
      privacy_redaction_summary: request.privacy_redaction_summary || request.privacyRedactionSummary || '',
      privacyRedacted: Boolean(request.privacy_redacted ?? request.privacyRedacted),
      privacyRedactionSummary: request.privacy_redaction_summary || request.privacyRedactionSummary || '',
      provider: request.provider || '',
      http_status_code: httpStatusCode,
      httpStatusCode,
      retry_count: retryCount,
      retryCount,
      method: request.method || '',
      path: request.path || '',
      client_ip: clientIP,
      clientIP,
      user_agent: userAgent,
      userAgent,
      channel: request.channel || '',
      endpoint,
      endpointChain,
      endpointChainHead,
      endpointChainTail,
      endpointChainLength,
      group: request.group_name || request.group || '',
      duration: request.duration_ms || request.duration || 0,
      firstByteMs: normalizeNumberOrNull(request.first_byte_ms ?? request.firstByteMs ?? request.first_byte_time ?? request.firstByteTime),
      inputTokens: request.input_tokens || request.inputTokens || 0,
      outputTokens: request.output_tokens || request.outputTokens || 0,
      reasoningTokens: request.reasoning_tokens ?? request.reasoningTokens ?? 0,
      billableOutputTokens: request.billable_output_tokens ?? request.billableOutputTokens ?? request.output_tokens ?? request.outputTokens ?? 0,
      billingOutputTokenSource: request.billing_output_token_source || request.billingOutputTokenSource || 'output_tokens',
	  billingRuleSnapshot: parseJSONObject(request.billing_rule_snapshot ?? request.billingRuleSnapshot),
      cacheCreationTokens: request.cache_creation_tokens || request.cacheCreationTokens || 0,
      cacheCreation5mTokens: request.cache_creation_5m_tokens || request.cacheCreation5mTokens || 0,
      cacheCreation1hTokens: request.cache_creation_1h_tokens || request.cacheCreation1hTokens || 0,
      cacheReadTokens: request.cache_read_tokens || request.cacheReadTokens || 0,
      cost: request.total_cost_usd || request.cost || 0,
      inputCost: request.input_cost_usd || request.inputCost || 0,
      outputCost: request.output_cost_usd || request.outputCost || 0,
      cacheCreationCost: request.cache_creation_cost_usd || request.cacheCreationCost || 0,
      cacheCreation5mCost: request.cache_creation_5m_cost_usd || request.cacheCreation5mCost || 0,
      cacheCreation1hCost: request.cache_creation_1h_cost_usd || request.cacheCreation1hCost || 0,
      cacheReadCost: request.cache_read_cost_usd || request.cacheReadCost || 0,
      // 端点倍率快照（计费详情展示用）
      costMultiplier: request.cost_multiplier ?? request.costMultiplier ?? 1.0,
      inputCostMultiplier: request.input_cost_multiplier ?? request.inputCostMultiplier ?? 1.0,
      outputCostMultiplier: request.output_cost_multiplier ?? request.outputCostMultiplier ?? 1.0,
      cacheCreationCostMultiplier: request.cache_creation_cost_multiplier ?? request.cacheCreationCostMultiplier ?? 1.0,
      cacheCreationCostMultiplier1h: request.cache_creation_cost_multiplier_1h ?? request.cacheCreationCostMultiplier1h ?? 1.0,
      cacheReadCostMultiplier: request.cache_read_cost_multiplier ?? request.cacheReadCostMultiplier ?? 1.0,
      modelCostMultiplier: request.model_cost_multiplier ?? request.modelCostMultiplier ?? 1.0,
      modelInputCostMultiplier: request.model_input_cost_multiplier ?? request.modelInputCostMultiplier ?? 1.0,
      modelOutputCostMultiplier: request.model_output_cost_multiplier ?? request.modelOutputCostMultiplier ?? 1.0,
      modelCacheReadCostMultiplier: request.model_cache_read_cost_multiplier ?? request.modelCacheReadCostMultiplier ?? 1.0,
      isStreaming: request.is_streaming ?? request.isStreaming ?? false,
      statusCode: httpStatusCode
    });
  };

  // Wails 环境使用绑定
  if (isWailsEnvironment()) {
    // 直接传递所有参数给 wailsApi
    const data = await WailsApi.getRequests(params);

    // 对 Wails 数据也进行规范化处理
    const requests = data.requests || [];
    const normalizedRequests = Array.isArray(requests) ? requests.map(normalizeRequest) : [];

    return {
      requests: normalizedRequests,
      total: data.total || normalizedRequests.length,
      page: data.page || 1,
      pageSize: data.pageSize || parseInt(params.limit || params.pageSize || 50),
      totalPages: data.totalPages || Math.ceil((data.total || 0) / (data.pageSize || parseInt(params.limit || params.pageSize || 50)))
    };
  }

  const queryParams = new URLSearchParams();
  Object.entries(params).forEach(([key, value]) => {
    if (value !== null && value !== undefined && value !== '') {
      queryParams.append(key, value.toString());
    }
  });

  // 确保固定排序参数
  if (!queryParams.has('sort_by')) {
    queryParams.set('sort_by', 'start_time');
  }
  if (!queryParams.has('sort_order')) {
    queryParams.set('sort_order', 'desc');
  }

  const url = queryParams.toString()
    ? `${API_ENDPOINTS.USAGE_REQUESTS}?${queryParams.toString()}`
    : API_ENDPOINTS.USAGE_REQUESTS;

  const data = await fetchWithTimeout(url);

  const requests = data.requests || data.data || data || [];
  const normalizedRequests = Array.isArray(requests) ? requests.map(normalizeRequest) : [];

  return {
    requests: normalizedRequests,
    total: data.total || normalizedRequests.length,
    page: data.page || 1,
    pageSize: data.pageSize || data.limit || 50,
    totalPages: data.totalPages || Math.ceil((data.total || 0) / (data.pageSize || data.limit || 50))
  };
};

/**
 * 获取模型列表（用于筛选器）
 * 从 model_pricing 表获取，而不是从使用记录
 * @returns {Promise<Array>} - 模型名称数组
 */
export const fetchModels = async () => {
  // Wails 环境：从 model_pricing 表获取
  if (isWailsEnvironment()) {
    try {
      const pricings = await WailsApi.getModelPricings();
      // 返回模型名称数组，保持向后兼容
      return (pricings || []).map(p => ({
        model_name: p.modelName,
        name: p.modelName,
        display_name: p.displayName || p.modelName
      }));
    } catch (err) {
      // Wails 桌面应用不应再访问 HTTP API（/api/v1/...），否则会在 WebView 里触发 404。
      // 模型列表仅用于筛选器，获取失败时返回空列表即可。
      console.warn('获取模型定价失败，返回空模型列表:', err);
      return [];
    }
  }

  // HTTP 环境或 Wails 降级：从使用记录获取
  const data = await fetchWithTimeout(API_ENDPOINTS.USAGE_MODELS);
  if (data.success && data.data) return data.data;
  if (data.models) return data.models;
  if (Array.isArray(data)) return data;
  return [];
};

// ============================================
// 配置 API
// ============================================

export const fetchConfig = async () => {
  return await fetchWithTimeout(API_ENDPOINTS.CONFIG);
};

// ============================================
// 图表数据 API
// ============================================

/**
 * 将 Chart.js 格式转换为 Recharts 格式
 * Chart.js: { labels: [...], datasets: [{data: [...]}, ...] }
 * Recharts: [{ time: '...', total: N, success: N, fail: N }, ...]
 */
const transformChartJsToRecharts = (chartJsData, keyMapping) => {
  if (!chartJsData?.labels || !chartJsData?.datasets) {
    return [];
  }

  const { labels, datasets } = chartJsData;
  return labels.map((label, index) => {
    const point = { time: label };
    datasets.forEach((dataset, datasetIndex) => {
      const key = keyMapping[datasetIndex] || `value${datasetIndex}`;
      point[key] = dataset.data?.[index] ?? 0;
    });
    return point;
  });
};

/**
 * 获取请求趋势数据
 * @param {number} minutes - 时间范围（分钟），默认 30
 * @param {string} provider - 提供商过滤（'claude'/'codex'/'')
 */
export const fetchRequestTrendData = async (minutes = 30, provider = '') => {
  try {
    // Wails 环境使用绑定
    if (isWailsEnvironment()) {
      return await WailsApi.getRequestTrendChart(minutes, provider);
    }

    const data = await fetchWithTimeout(`/api/v1/chart/request-trends?minutes=${minutes}`);
    // 转换为 Recharts 格式: total, success, fail
    return transformChartJsToRecharts(data, ['total', 'success', 'fail']);
  } catch (error) {
    console.error('获取请求趋势数据失败:', error);
    return [];
  }
};

/**
 * 获取响应时间数据
 * @param {number} minutes - 时间范围（分钟），默认 30
 * @param {string} provider - 提供商过滤（'claude'/'codex'/'')
 */
export const fetchResponseTimeData = async (minutes = 30, provider = '') => {
  try {
    // Wails 环境使用绑定
    if (isWailsEnvironment()) {
      return await WailsApi.getResponseTimeChart(minutes, provider);
    }

    const data = await fetchWithTimeout(`/api/v1/chart/response-times?minutes=${minutes}`);
    // 转换为 Recharts 格式: avg, min, max
    return transformChartJsToRecharts(data, ['avg', 'min', 'max']);
  } catch (error) {
    console.error('获取响应时间数据失败:', error);
    return [];
  }
};

/**
 * 获取 Token 使用数据
 * @param {string} provider - 提供商过滤（'claude'/'codex'/'')
 */
export const fetchTokenUsageData = async (provider = '') => {
  try {
    // Wails 环境使用绑定
    if (isWailsEnvironment()) {
      return await WailsApi.getTokenUsage(provider);
    }

    const data = await fetchWithTimeout('/api/v1/tokens/usage');
    const current = data.current || data;
    return {
      input: current.input_tokens || 0,
      output: current.output_tokens || 0,
      cacheCreation: current.cache_creation_tokens || 0,
      cacheRead: current.cache_read_tokens || 0,
      total: current.total_tokens || 0
    };
  } catch (error) {
    console.error('获取 Token 使用数据失败:', error);
    throw error;
  }
};

/**
 * 获取按请求追踪条件聚合的 Token 使用分布
 * @param {Object} params - 请求追踪查询参数
 */
export const fetchTokenUsageHeatmapData = async (params = {}) => {
  try {
    if (isWailsEnvironment()) {
      return await WailsApi.getTokenUsageHeatmap(params);
    }
    return [];
  } catch (error) {
    console.error('获取 Token 使用分布数据失败:', error);
    throw error;
  }
};

/**
 * 获取端点健康状态数据
 */
export const fetchEndpointHealthData = async () => {
  try {
    // Wails 环境使用绑定
    if (isWailsEnvironment()) {
      return await WailsApi.getEndpointHealthChart();
    }

    const data = await fetchWithTimeout('/api/v1/chart/endpoint-health');
    // 返回 { healthy: N, unhealthy: N, unchecked?: N } 或原始 Chart.js 格式
    if (data.labels && data.datasets) {
      const [healthy, unhealthy] = data.datasets[0]?.data || [0, 0];
      return { healthy, unhealthy, unchecked: 0 };
    }
    return {
      healthy: data.healthy || 0,
      unhealthy: data.unhealthy || 0,
      unchecked: data.unchecked || 0,
      skipped: data.skipped || 0
    };
  } catch (error) {
    console.error('获取端点健康状态数据失败:', error);
    return { healthy: 0, unhealthy: 0, unchecked: 0, skipped: 0 };
  }
};

/**
 * 获取端点成本数据
 * @param {string} provider - 提供商过滤（'claude'/'codex'/'')
 */
export const fetchEndpointCostsData = async (provider = '') => {
  try {
    // Wails 环境使用绑定
    if (isWailsEnvironment()) {
      return await WailsApi.getEndpointCosts(provider);
    }

    const data = await fetchWithTimeout('/api/v1/chart/endpoint-costs');
    // 转换为 Recharts 格式: { name, tokens, cost }
    if (data.labels && data.datasets) {
      // 查找 Token 数据集（label 包含 "Token" 或 "token"）
      const tokensDataset = data.datasets.find(d =>
        d.label?.toLowerCase().includes('token')
      );
      // 查找成本数据集（label 包含 "成本" 或 "Cost" 或 "USD"）
      const costDataset = data.datasets.find(d =>
        d.label?.includes('成本') || d.label?.toLowerCase().includes('cost') || d.label?.includes('USD')
      );

      const tokensData = tokensDataset?.data || [];
      const costData = costDataset?.data || [];

      return data.labels.map((name, i) => ({
        name,
        channel: '',
        endpoint: name,
        group: '',
        tokens: tokensData[i] || 0,
        cost: costData[i] || 0
      }));
    }
    return [];
  } catch (error) {
    console.error('获取端点成本数据失败:', error);
    throw error;
  }
};

/**
 * 获取连接活动数据
 * @param {number} minutes - 时间范围（分钟），默认 60
 * @param {string} provider - 提供商过滤（'claude'/'codex'/'')
 */
export const fetchConnectionActivityData = async (minutes = 60, provider = '') => {
  try {
    // Wails 环境使用绑定
    if (isWailsEnvironment()) {
      return await WailsApi.getConnectionActivityChart(minutes, provider);
    }

    const data = await fetchWithTimeout(`/api/v1/chart/connection-activity?minutes=${minutes}`);
    return transformChartJsToRecharts(data, ['connections']);
  } catch (error) {
    console.error('获取连接活动数据失败:', error);
    return [];
  }
};

// ============================================
// 导出工具函数
// ============================================

export const formatUptime = (seconds) => {
  if (typeof seconds !== 'number' || seconds <= 0) return seconds;

  const hours = Math.floor(seconds / 3600);
  const minutes = Math.floor((seconds % 3600) / 60);
  const secs = Math.floor(seconds % 60);

  let result = '';
  if (hours > 0) result += `${hours}h `;
  if (minutes > 0) result += `${minutes}m `;
  if (secs > 0 || result === '') result += `${secs}s`;

  return result.trim();
};

export const formatNumber = (num) => {
  if (num >= 1000000) return (num / 1000000).toFixed(1) + 'M';
  if (num >= 1000) return (num / 1000).toFixed(1) + 'K';
  return num.toString();
};

export const formatCost = (cost) => {
  if (cost >= 1) return `$${cost.toFixed(2)}`;
  if (cost >= 0.01) return `$${cost.toFixed(3)}`;
  return `$${cost.toFixed(4)}`;
};

export const formatDuration = (ms) => {
  if (ms >= 1000) return `${(ms / 1000).toFixed(2)}s`;
  return `${ms}ms`;
};

// 格式化时间戳
// 后端返回的时间已经是配置时区的时间（如北京时间），格式：2025-12-04 17:18:48
// 直接格式化显示，不做时区转换
export const formatTimestamp = (timestamp) => {
  if (!timestamp) return 'N/A';

  // 如果是纯时间格式（无T、无时区），直接格式化显示
  // 格式：2025-12-04 17:18:48 → 2025/12/4 17:18:48
  if (typeof timestamp === 'string' && !timestamp.includes('T') && !timestamp.includes('+') && !timestamp.includes('Z')) {
    // 解析 "2025-12-04 17:18:48" 格式
    const match = timestamp.match(/^(\d{4})-(\d{2})-(\d{2})\s+(\d{2}):(\d{2}):(\d{2})/);
    if (match) {
      const [, year, month, day, hours, minutes, seconds] = match;
      // 直接显示，不转换时区
      return `${year}/${parseInt(month)}/${parseInt(day)} ${hours}:${minutes}:${seconds}`;
    }
  }

  // 带时区的格式（RFC3339），用 Date 解析
  const date = new Date(timestamp);
  if (isNaN(date.getTime())) return 'Invalid Date';

  const year = date.getFullYear();
  const month = date.getMonth() + 1;
  const day = date.getDate();
  const hours = String(date.getHours()).padStart(2, '0');
  const minutes = String(date.getMinutes()).padStart(2, '0');
  const seconds = String(date.getSeconds()).padStart(2, '0');

  return `${year}/${month}/${day} ${hours}:${minutes}:${seconds}`;
};

// ============================================
// 系统设置 API (SQLite)
// ============================================

/**
 * 获取设置分类列表
 * @returns {Promise<Array>} - 分类信息数组
 */
export const fetchSettingCategories = async () => {
  // Wails 环境使用绑定
  if (isWailsEnvironment()) {
    return await WailsApi.getSettingCategories();
  }
  // HTTP 环境
  const data = await fetchWithTimeout('/api/v1/settings/categories');
  return data.categories || data || [];
};

/**
 * 获取所有设置
 * @returns {Promise<Array>} - 设置记录数组
 */
export const fetchAllSettings = async () => {
  // Wails 环境使用绑定
  if (isWailsEnvironment()) {
    return await WailsApi.getAllSettings();
  }
  // HTTP 环境
  const data = await fetchWithTimeout('/api/v1/settings');
  return data.settings || data || [];
};

/**
 * 批量更新设置
 * @param {Array} settings - [{category, key, value}, ...]
 * @returns {Promise<Object>}
 */
export const updateSettings = async (settings) => {
  // Wails 环境使用绑定
  if (isWailsEnvironment()) {
    return await WailsApi.batchUpdateSettings({ settings });
  }
  // HTTP 环境
  return await fetchWithTimeout('/api/v1/settings', {
    method: 'PUT',
    body: JSON.stringify({ settings })
  });
};

/**
 * 重置分类设置为默认值
 * @param {string} category - 分类名称
 * @returns {Promise<Object>}
 */
export const resetSettings = async (category) => {
  // Wails 环境使用绑定
  if (isWailsEnvironment()) {
    return await WailsApi.resetCategorySettings(category);
  }
  // HTTP 环境
  return await fetchWithTimeout(`/api/v1/settings/${encodeURIComponent(category)}/reset`, {
    method: 'POST'
  });
};

/**
 * 获取端口信息
 * @returns {Promise<Object>} - {preferred_port, actual_port, was_occupied}
 */
export const fetchPortInfo = async () => {
  // Wails 环境使用绑定
  if (isWailsEnvironment()) {
    return await WailsApi.getPortInfo();
  }
  // HTTP 环境
  const data = await fetchWithTimeout('/api/v1/settings/port');
  return data;
};

const requirePrivacyWails = () => {
  if (!isWailsEnvironment()) {
    throw new Error('HTTP 环境暂不支持隐私防护功能');
  }
};

export const fetchPrivacyOverview = async () => {
  requirePrivacyWails();
  return await WailsApi.getPrivacyOverview();
};

export const updatePrivacySettings = async (settings) => {
  requirePrivacyWails();
  return await WailsApi.updatePrivacySettings(settings);
};

export const createPrivacyRule = async (rule) => {
  requirePrivacyWails();
  return await WailsApi.createPrivacyRule(rule);
};

export const updatePrivacyRule = async (id, rule) => {
  requirePrivacyWails();
  return await WailsApi.updatePrivacyRule(id, rule);
};

export const deletePrivacyRule = async (id) => {
  requirePrivacyWails();
  return await WailsApi.deletePrivacyRule(id);
};

export const applyPrivacyPreset = async (presetId) => {
  requirePrivacyWails();
  return await WailsApi.applyPrivacyPreset(presetId);
};

export const discardLegacyPrivacyRules = async () => {
  requirePrivacyWails();
  return await WailsApi.discardLegacyPrivacyRules();
};

export const previewPrivacyRedaction = async (input) => {
  requirePrivacyWails();
  return await WailsApi.previewPrivacyRedaction(input);
};

export const fetchPrivacyUnscannedPaths = async () => {
  requirePrivacyWails();
  return await WailsApi.getPrivacyUnscannedPaths();
};

export const clearPrivacyUnscannedPaths = async () => {
  requirePrivacyWails();
  return await WailsApi.clearPrivacyUnscannedPaths();
};
