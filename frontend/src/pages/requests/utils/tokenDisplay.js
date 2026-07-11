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

export const getDisplayTotalTokens = (request) => {
  const outputTokens = numeric(request?.outputTokens ?? request?.output_tokens);
  const cacheCreationTokens = numeric(request?.cacheCreationTokens ?? request?.cache_creation_tokens);
  const cacheReadTokens = numeric(request?.cacheReadTokens ?? request?.cache_read_tokens);

  if (isCacheInclusiveProvider(request?.provider)) {
    return getDisplayInputTokens(request) + outputTokens;
  }

  return getDisplayInputTokens(request) + outputTokens + cacheCreationTokens + cacheReadTokens;
};
