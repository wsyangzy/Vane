export const parseEndpointChain = (value) => {
  if (Array.isArray(value)) return value;
  if (typeof value !== 'string') return [];

  const raw = value.trim();
  if (!raw) return [];

  if (raw.startsWith('[')) {
    try {
      const parsed = JSON.parse(raw);
      return Array.isArray(parsed) ? parsed : [];
    } catch {
      // Fall through to legacy delimiter parsing.
    }
  }

  return raw.split(' -> ').map((item) => item.trim()).filter(Boolean);
};

export const parseChainNode = (item, hop) => {
  if (!item) return { raw: '', channel: '', credentialGroup: '', endpoint: '', hop };

  if (typeof item === 'object' && item !== null) {
    const channel = item.channel || '';
    const credentialGroup = item.credential_group || item.credentialGroup || item.group_name || item.group || '';
    const endpoint = item.endpoint || '';
    const statusCode = Number(item.status_code ?? item.statusCode ?? item.http_status ?? item.httpStatus ?? 0) || 0;
    // 只认后端写入链路节点的原始错误文本；failure_reason 是 Relay 内部分类，不做展示。
    const error = item.error || item.error_message || item.errorMessage || '';
    // 本跳实际发出的请求次数；旧记录没有该字段，为 0 表示未知
    const attempts = Math.max(0, Math.trunc(Number(item.attempts ?? 0) || 0));
    // 本跳耗时；旧记录与进行中的最后一跳没有，为 null
    const rawDuration = item.duration_ms ?? item.durationMs;
    const durationMs = rawDuration === null || rawDuration === undefined || !Number.isFinite(Number(rawDuration))
      ? null
      : Math.max(0, Number(rawDuration));
    // 失败响应的诊断头白名单（server、cf-ray、x-request-id 等）；旧记录没有
    const headers = item.headers && typeof item.headers === 'object' && !Array.isArray(item.headers) ? item.headers : {};
    // 本跳没有拿到上游响应（网络/超时），error 是本地错误文本
    const local = Boolean(item.local);
    const raw = channel
      ? `${channel}::${credentialGroup ? `${credentialGroup}::` : ''}${endpoint}`
      : endpoint;
    return {
      raw,
      channel,
      credentialGroup,
      endpoint,
      statusCode,
      error,
      attempts,
      durationMs,
      headers,
      local,
      hop: item.hop || hop,
    };
  }

  const raw = String(item || '').trim();
  if (!raw) return { raw: '', channel: '', credentialGroup: '', endpoint: '', hop };

  const parts = raw.split('::');
  if (parts.length >= 3) {
    return {
      raw,
      channel: parts[0] || '',
      credentialGroup: parts[1] || '',
      endpoint: parts.slice(2).join('::') || '',
      hop,
    };
  }
  if (parts.length === 2) {
    return {
      raw,
      channel: parts[0] || '',
      credentialGroup: '',
      endpoint: parts[1] || '',
      hop,
    };
  }

  return { raw, channel: '', credentialGroup: '', endpoint: parts[0] || '', hop };
};

export const getEndpointChainItems = (request) => {
  const normalized = parseEndpointChain(request?.endpointChain);
  if (normalized.length > 0) return normalized;
  return parseEndpointChain(request?.endpoint_chain);
};

export const getRequestCredentialGroupLabel = (request = {}, chainNodes = []) => {
  const credentialGroup = String(chainNodes[chainNodes.length - 1]?.credentialGroup || '').trim();
  if (credentialGroup) return credentialGroup;

  const directGroup = String(request.credentialGroup || request.groupName || request.group_name || '').trim();
  if (directGroup) return directGroup;

  if (String(request.provider || '').toLowerCase() === 'codex') {
    return String(request.group || '').trim() || '-';
  }
  return '-';
};

export const getRequestChannelLabel = (request = {}, chainNodes = [], fallback = '-') => {
  const directChannel = String(request.channel || '').trim();
  if (directChannel) return directChannel;

  for (let index = chainNodes.length - 1; index >= 0; index -= 1) {
    const channel = String(chainNodes[index]?.channel || '').trim();
    if (channel) return channel;
  }

  if (String(request.provider || '').toLowerCase() !== 'codex') {
    return String(request.group || '').trim() || fallback;
  }
  return fallback;
};

export const chainHasChannel = (items) => (
  items.some((item) => Boolean(parseChainNode(item).channel))
);

export const chainHasMultipleChannels = (items) => {
  const channels = new Set();
  items.forEach((item) => {
    const channel = parseChainNode(item).channel;
    if (channel) channels.add(channel);
  });
  return channels.size > 1;
};

export const countChannelSwitches = (items = []) => {
  if (!Array.isArray(items)) return 0;
  return items.reduce((count, item, index) => {
    if (index === 0) return count;
    const previousChannel = parseChainNode(items[index - 1]).channel;
    const currentChannel = parseChainNode(item).channel;
    return count + (previousChannel && currentChannel && previousChannel !== currentChannel ? 1 : 0);
  }, 0);
};

export const requestHasInterChannelTransfer = (request = {}) => (
  chainHasMultipleChannels(getEndpointChainItems(request))
);

export const summarizeEndpointAttempts = (request = {}, parsedNodes) => {
  const nodes = parsedNodes || getEndpointChainItems(request).map((item, index) => parseChainNode(item, index + 1));
  const directEndpoint = request.endpoint || request.endpoint_name || '';
  const rawRetryCount = Number(request.retry_count ?? request.retryCount ?? 0);
  const storedRetryCount = Number.isFinite(rawRetryCount) ? Math.max(0, Math.trunc(rawRetryCount)) : 0;
  const retryCount = Math.max(storedRetryCount, nodes.length - 1);
  let transferCount = 0;

  for (let index = 1; index < nodes.length; index += 1) {
    const previous = nodes[index - 1];
    const current = nodes[index];
    const previousIdentity = [previous.channel, previous.credentialGroup, previous.endpoint].join('\u0000');
    const currentIdentity = [current.channel, current.credentialGroup, current.endpoint].join('\u0000');
    if (previousIdentity !== currentIdentity) transferCount += 1;
  }

  // 最终端点内的尝试次数：只有新链路记录了每跳次数；旧记录无从得知，返回 null 由展示层显示 "-"
  const finalAttempts = nodes[nodes.length - 1]?.attempts || 0;

  return {
    attemptCount: Math.max(1, retryCount + 1, nodes.length),
    finalEndpointAttempts: finalAttempts > 0 ? finalAttempts : null,
    retryCount,
    transferCount,
    transferred: transferCount > 0,
    firstEndpoint: nodes[0]?.endpoint || directEndpoint,
    finalEndpoint: nodes[nodes.length - 1]?.endpoint || directEndpoint,
  };
};

export const getRequestNetworkDisplayState = (request = {}) => {
  const rawDuration = request.duration ?? request.duration_ms;
  const duration = rawDuration === null || rawDuration === undefined || rawDuration === '' ? null : Number(rawDuration);
  const rawStreaming = request.isStreaming ?? request.is_streaming;
  return {
    durationMs: Number.isFinite(duration) ? duration : null,
    isStreaming: typeof rawStreaming === 'boolean' ? rawStreaming : null,
  };
};

export const parseEndpointName = (item) => parseChainNode(item).endpoint;

// 错误卡片要展示的失败跳：有状态码、错误文本或诊断头的节点，最后一跳在前。
// 多跳失败时每一跳的上游原文都要能直接看到，不能只剩最终那一跳。
export const collectFailedHops = (nodes = []) => nodes
  .filter((node) => node && (node.statusCode > 0 || node.error || Object.keys(node.headers || {}).length > 0))
  .slice()
  .reverse();

// 上游错误原文若是 JSON，格式化为缩进文本；否则原样返回。不改动任何内容，只换排版。
export const formatUpstreamErrorText = (text) => {
  const raw = String(text ?? '');
  const trimmed = raw.trim();
  if (!trimmed || (trimmed[0] !== '{' && trimmed[0] !== '[')) return { text: raw, isJSON: false };
  try {
    return { text: JSON.stringify(JSON.parse(trimmed), null, 2), isJSON: true };
  } catch {
    return { text: raw, isJSON: false };
  }
};

// 从上游错误原文里挑出最能说明原因的一句（error.message / message / error 字符串），失败时返回 ''
export const extractUpstreamErrorMessage = (text) => {
  const trimmed = String(text ?? '').trim();
  if (!trimmed.startsWith('{')) return '';
  try {
    const parsed = JSON.parse(trimmed);
    const candidates = [parsed?.error?.message, parsed?.message, typeof parsed?.error === 'string' ? parsed.error : ''];
    return candidates.find((value) => typeof value === 'string' && value.trim()) || '';
  } catch {
    return '';
  }
};
