const numeric = (value) => {
  const number = Number(value ?? 0);
  return Number.isFinite(number) ? number : 0;
};

export const isCacheInclusiveProvider = (provider) => {
  const value = String(provider || '').toLowerCase();
  return value === 'codex' || value === 'openai';
};

export const getDisplayInputTokens = (request) => {
  const inputTokens = numeric(request?.inputTokens ?? request?.input_tokens);
  const cacheReadTokens = numeric(request?.cacheReadTokens ?? request?.cache_read_tokens);

  if (isCacheInclusiveProvider(request?.provider) && cacheReadTokens > inputTokens) {
    return inputTokens + cacheReadTokens;
  }
  return inputTokens;
};

export const getBillableInputTokens = (request) => {
  const inputTokens = numeric(request?.inputTokens ?? request?.input_tokens);
  const cacheReadTokens = numeric(request?.cacheReadTokens ?? request?.cache_read_tokens);
  const cacheWriteTokens = numeric(request?.cacheCreationTokens ?? request?.cache_creation_tokens);

  if (!isCacheInclusiveProvider(request?.provider)) {
    return inputTokens;
  }
  const cachedInputTokens = cacheReadTokens + cacheWriteTokens;
  if (cachedInputTokens > inputTokens) {
    return inputTokens;
  }
  return Math.max(0, inputTokens - cachedInputTokens);
};

// 单条请求的缓存命中率，与后端 cacheHitParts 同口径：
//   Claude：cache_read / (input + cache_creation + cache_read)
//   Codex ：cache_read / input（input 已含缓存；极旧行 cache_read > input 时分母补回 cache_read）
// 没有输入用量时返回 null
export const getRequestCacheHitRate = (request) => {
  const cacheReadTokens = numeric(request?.cacheReadTokens ?? request?.cache_read_tokens);
  const denominator = isCacheInclusiveProvider(request?.provider)
    ? getDisplayInputTokens(request)
    : numeric(request?.inputTokens ?? request?.input_tokens)
      + numeric(request?.cacheCreationTokens ?? request?.cache_creation_tokens)
      + cacheReadTokens;
  return denominator > 0 ? cacheReadTokens / denominator : null;
};

// 缓存命中率（后端按 provider 口径计算，0~1）；null 表示没有输入用量，与真实的 0% 区分
export const formatCacheHitRate = (rate) => {
  if (rate === null || rate === undefined || rate === '') return '-';
  const num = Number(rate);
  if (!Number.isFinite(num)) return '-';
  return `${(num * 100).toFixed(1)}%`;
};

export const getDisplayTotalTokens = (request) => {
  const outputTokens = numeric(request?.outputTokens ?? request?.output_tokens);
  const cacheCreationTokens = numeric(request?.cacheCreationTokens ?? request?.cache_creation_tokens);
  const cacheReadTokens = numeric(request?.cacheReadTokens ?? request?.cache_read_tokens);

  if (isCacheInclusiveProvider(request?.provider)) {
    return getDisplayInputTokens(request) + outputTokens;
  }

  return getDisplayInputTokens(request) + outputTokens + cacheCreationTokens + cacheReadTokens;
};
