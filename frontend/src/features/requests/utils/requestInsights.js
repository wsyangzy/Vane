import { getBillableInputTokens, getDisplayInputTokens, isCacheInclusiveProvider } from './tokenDisplay.js';
import { getEndpointChainItems, isCodexSearchRequest, parseChainNode, extractUpstreamErrorMessage } from './endpointChain.js';
import { getRequestDisplayStatus, isRequestInProgress } from './requestOutcome.js';

const amount = value => Math.max(0, Number.isFinite(Number(value)) ? Number(value) : 0);
const optionalAmount = value => value == null ? null : amount(value);
const successful = status => ['completed', 'success', 'succeeded'].includes(status);
const currency = new Intl.NumberFormat('en-US', { style: 'currency', currency: 'USD', maximumFractionDigits: 8 });

export function formatRequestInsightCost(value) {
  if (value == null) return '—';
  if (value !== 0 && Math.abs(value) < 1e-8) return value < 0 ? '>−$0.00000001' : '<$0.00000001';
  return currency.format(value);
}

const contextCategories = {
  system: ['系统提示词', 'violet'],
  tools: ['工具定义', 'blue'],
  memory: ['项目说明', 'amber'],
  skills: ['技能', 'slate'],
  chat: ['会话内容', 'indigo'],
  files: ['文件与附件', 'teal'],
  results: ['工具结果', 'green'],
  summary: ['压缩摘要', 'purple'],
};

const contextEntryLabels = {
  prompt: 'Agent 提示词',
  developer: '开发者指令',
  system: '系统指令',
  user_instructions: '权限与用户指令',
  environment: '环境上下文',
  instruction: '项目说明',
  skill: '技能清单',
  tool: '工具定义',
  namespace: '工具命名空间',
  mcp: 'MCP 工具',
  file: '文件',
  image: '图片',
  result: '工具结果',
  compaction: '压缩摘要',
  other: '其余条目',
};

// Dispatch events describe individual sends. Legacy chain nodes can aggregate
// retries; never expand their total timing into invented per-attempt timings.
export function buildFallbackAttempts(request) {
  const trace = request.dispatchTrace;
  const status = getRequestDisplayStatus(request);
  const searchRequest = isCodexSearchRequest(request);
  const endpointChain = getEndpointChainItems(request);
  const source = searchRequest && endpointChain.length ? endpointChain
    : trace?.attemptCount === 0
      ? (trace.skipped || []).filter(item => item.reason !== 'candidate_duplicate').map(item => ({ ...item, skipReason: item.reason || '未发送' }))
      : trace?.attempts?.length ? trace.attempts : endpointChain;
  const nodes = source.map((item, index) => ({ ...parseChainNode(item, index + 1), ...(typeof item === 'object' ? item : {}) }));
  if (!nodes.length && !trace && request.endpoint) nodes.push({ endpoint: request.endpoint, channel: request.channel });
  return nodes.map((node, index) => {
    const last = index === nodes.length - 1;
    const searchServiceCall = searchRequest && node.channel === '搜索服务';
    const isTavily = searchServiceCall && /tavily/i.test(node.endpoint || '');
    const rawUsageCredits = node.usageCredits ?? node.usage_credits;
    const usageCredits = rawUsageCredits == null ? null : amount(rawUsageCredits);
    let state = 'unknown';
    if (node.skipReason) state = 'skipped';
    else if (searchServiceCall && (node.local || node.error || node.statusCode >= 400)) state = 'failed';
    else if (searchServiceCall && node.statusCode >= 200 && node.statusCode < 300) state = 'answered';
    else if (node.action === 'success' || (last && (trace?.successful || successful(status)))) state = 'answered';
    else if (last && ['canceled', 'cancelled'].includes(status)) state = 'cancelled';
    else if (last && status === 'incomplete') state = 'incomplete';
    else if (node.action === 'failed' || node.statusCode >= 400 || node.local || node.error) state = 'failed';
    else if (last && isRequestInProgress(request)) state = 'trying';
    else if (!last || ['failed', 'error', 'timeout'].includes(status)) state = 'failed';
    return {
      id: `${index}`, channel: node.channel || (nodes.length === 1 ? request.channel : ''),
      group: node.credentialGroup || '', endpoint: node.endpoint || '未记录端点',
      model: node.model || request.model, state, statusCode: node.statusCode || null,
      duration: optionalAmount(node.durationMs), attempts: amount(node.attempts),
      isTavily, usageCredits,
      reason: extractUpstreamErrorMessage(node.error) || node.reason || node.skipReason || '',
    };
  });
}

export function summarizeTavilyUsage(attempts) {
  const calls = attempts.filter(item => item.isTavily);
  const reported = calls.filter(item => item.usageCredits != null);
  return {
    calls: calls.length,
    reportedCalls: reported.length,
    credits: reported.length ? reported.reduce((sum, item) => sum + item.usageCredits, 0) : null,
  };
}

export function buildContextMap(request) {
  const read = amount(request.cacheReadTokens);
  const write = amount(request.cacheCreationTokens);
  const inputEstimated = Boolean(request.billingRuleSnapshot?.live_input_estimated);
  const input = inputEstimated ? 0
    : isCacheInclusiveProvider(request.provider) ? getDisplayInputTokens(request) : amount(request.inputTokens) + read + write;
  const summary = request.contextSummary;
  const detailed = summary?.version === 2;
  const estimated = summary?.version === 1 || detailed;
  const partial = estimated && Boolean(summary.partial);
  const rows = estimated
    ? Object.entries(contextCategories).map(([id, [label, tone]]) => ({
      id, label, tone, tokens: amount(summary.parts?.[id]?.tokens), items: amount(summary.parts?.[id]?.items),
    }))
    : [
      { id: 'input', label: '未缓存输入', tone: 'indigo', tokens: getBillableInputTokens(request) },
      { id: 'read', label: '缓存读取', tone: 'blue', tokens: read },
      { id: 'write', label: '缓存创建 / 写入', tone: 'purple', tokens: write },
    ];
  const visible = rows.filter(row => row.tokens > 0).sort((a, b) => b.tokens - a.tokens);
  const estimatedTotal = visible.reduce((sum, row) => sum + row.tokens, 0);
  const calibrated = detailed && !partial && input > 0 && estimatedTotal > 0;
  let running = 0;
  const groups = visible.map(row => {
    const next = running + row.tokens;
    const tokens = calibrated
      ? Math.round(next * input / estimatedTotal) - Math.round(running * input / estimatedTotal)
      : row.tokens;
    running = next;
    const breakdown = summary?.parts?.[row.id]?.breakdown;
    if (!detailed || !Array.isArray(breakdown) || !breakdown.length) return { ...row, tokens };
    const groupEstimate = breakdown.reduce((sum, item) => sum + amount(item.tokens), 0);
    let groupRunning = 0;
    const entries = breakdown.map((item, index) => {
      const estimate = amount(item.tokens);
      const entryNext = groupRunning + estimate;
      const entryTokens = calibrated && groupEstimate > 0
        ? Math.round(entryNext * tokens / groupEstimate) - Math.round(groupRunning * tokens / groupEstimate)
        : estimate;
      groupRunning = entryNext;
      return {
        id: `${row.id}-${item.kind}-${item.name ?? item.turn ?? index}`, group: row.id, tone: row.tone, kind: item.kind,
        label: item.kind === 'turn' ? `第 ${item.turn} 轮` : item.name || contextEntryLabels[item.kind] || row.label,
        tokens: entryTokens, items: amount(item.count),
      };
    });
    return { ...row, tokens, entries };
  });
  return {
    input, estimated, partial, calibrated, turns: detailed ? amount(summary.turns) : 0,
    rows: groups, total: groups.reduce((sum, row) => sum + row.tokens, 0),
  };
}

export function contextTileRows(rows) {
  const entries = rows.flatMap(row => row.entries || []).sort((a, b) => b.tokens - a.tokens);
  const largest = new Set(entries.slice(0, 12));
  return rows.flatMap(row => {
    const named = (row.entries || []).filter(entry => entry.tokens > 0 && largest.has(entry));
    const remaining = row.tokens - named.reduce((sum, item) => sum + item.tokens, 0);
    return remaining > 0 ? [...named, { ...row, tokens: remaining }] : named;
  }).sort((a, b) => b.tokens - a.tokens);
}

// Split the largest groups across the longest side of a fixed logical canvas.
// Percent coordinates also work at narrow widths without measuring the DOM.
export function layoutContextTiles(rows, total) {
  const tiles = [];
  function split(start, end, sum, x, y, width, height) {
    if (end - start === 1) {
      tiles.push({ ...rows[start], x, y, width, height });
      return;
    }
    let midpoint = start + 1;
    let left = rows[start].tokens;
    while (midpoint < end - 1 && left + rows[midpoint].tokens / 2 < sum / 2) {
      left += rows[midpoint].tokens;
      midpoint++;
    }
    if (width >= height * 0.38) {
      const cut = width * left / sum;
      split(start, midpoint, left, x, y, cut, height);
      split(midpoint, end, sum - left, x + cut, y, width - cut, height);
    } else {
      const cut = height * left / sum;
      split(start, midpoint, left, x, y, width, cut);
      split(midpoint, end, sum - left, x, y + cut, width, height - cut);
    }
  }
  if (rows.length && total > 0) split(0, rows.length, total, 0, 0, 100, 100);
  return tiles;
}

function outputCostComponents(request, billedTokens) {
  const { billingOutputTokenSource: source, outputCost } = request;
  const raw = amount(request.outputTokens);
  const reasoning = amount(request.reasoningTokens);
  const number = value => value.toLocaleString('en-US');
  if (source === 'reasoning_tokens') {
    return [['output-reasoning', '推理输出', 'teal', billedTokens, outputCost, 'output', '仅推理 Token 计费']];
  }
  if (source === 'visible_output_tokens') {
    const detail = reasoning > 0 && raw - reasoning === billedTokens
      ? `${number(raw)} − ${number(reasoning)} = ${number(billedTokens)} Token`
      : '按可见输出计费';
    return [['output-visible', '可见输出', 'green', billedTokens, outputCost, 'output', detail]];
  }
  if (isCacheInclusiveProvider(request.provider) && raw === billedTokens && reasoning > 0 && reasoning <= raw) {
    // The recorded output cost uses one rate for both kinds of output.
    const cost = amount(outputCost);
    const reasoningCost = cost * (reasoning / raw);
    return [
      ['output-visible', '可见输出', 'green', raw - reasoning, cost - reasoningCost, 'output'],
      ['output-reasoning', '推理输出', 'teal', reasoning, reasoningCost, 'output'],
    ];
  }
  return [['output', '计费输出', 'green', billedTokens, outputCost, 'output']];
}

// Use recorded costs, including historical split-cache and Codex write TTLs.
export function buildCostSteps(request) {
  const snapshot = request.billingRuleSnapshot || {};
  const output = amount(request.billableOutputTokens ?? request.outputTokens);
  const writeTokens = amount(request.cacheCreationTokens);
  const ttlTokens = amount(snapshot.cache_write_30m_tokens) + amount(snapshot.cache_write_12h_tokens);
  const writeTTLs = Boolean(snapshot.cache_write_pricing) && ttlTokens > 0 && (writeTokens === 0 || writeTokens === ttlTokens);
  const split = !writeTTLs && amount(request.cacheCreation5mTokens) + amount(request.cacheCreation1hTokens) > 0;
  const inputComponents = [
    ['input', '未缓存输入', 'blue', getBillableInputTokens(request), request.inputCost, 'input'],
    ['read', '缓存读取', 'amber', request.cacheReadTokens, request.cacheReadCost, 'cache_read'],
    ...(writeTTLs ? ['30m', '12h'].map(ttl => {
      const key = `cache_write_${ttl}`;
      const price = ttl === '30m' && snapshot.cache_write_30m_price == null ? 'cache_creation' : key;
      return [`write-${ttl}`, `${ttl} 缓存写入`, 'purple', snapshot[`${key}_tokens`], snapshot[`${key}_cost`], price];
    }) : split ? [
      ['write-5m', '5m 缓存创建', 'purple', request.cacheCreation5mTokens, request.cacheCreation5mCost, 'cache_creation'],
      ['write-1h', '1h 缓存创建', 'violet', request.cacheCreation1hTokens, request.cacheCreation1hCost, 'cache_creation_1h'],
    ] : [['write', '缓存创建 / 写入', 'purple', request.cacheCreationTokens, request.cacheCreationCost, 'cache_creation']]),
  ];
  const unpriced = snapshot.pricing_fallback === 'missing_price';
  const billingRow = ([id, label, tone, rawTokens, cost, price, detail], isOutput) => {
    const tokens = amount(rawTokens);
    const multiplier = price.startsWith('cache_write_') ? 'cache_creation' : price;
    return {
      id, label, tone, detail, tokens, cost: amount(cost),
      input: isOutput ? null : tokens,
      output: isOutput ? tokens : null,
      pricing: unpriced || snapshot[`${price}_price`] == null ? null : {
        unit: amount(snapshot[`${price}_price`]),
        multiplier: snapshot[`${multiplier}_cost_multiplier`] ?? null,
        totalMultiplier: snapshot.total_cost_multiplier ?? null,
      },
    };
  };
  const rows = [
    ...inputComponents.map(component => billingRow(component, false)),
    ...outputCostComponents(request, output).map(component => billingRow(component, true)),
  ].filter(row => row.tokens > 0 || row.cost > 0);
  const total = amount(request.cost);
  const sum = rows.reduce((value, row) => value + row.cost, 0);
  const residual = total - sum;
  if (Math.abs(residual) > Math.max(1e-9, total * 1e-6)) rows.push({ id: 'residual', label: '历史成本差额', tone: 'slate', tokens: 0, cost: residual, residual: true });
  const input = rows.reduce((value, row) => value + (row.input ?? 0), 0);
  return {
    rows, total, input, output, tokens: input + output, unpriced,
    inputEstimated: Boolean(snapshot.live_input_estimated),
    outputEstimated: Boolean(snapshot.live_output_estimated),
  };
}

export function buildContextCostEstimate(request, billing) {
  if (billing.rows.some(row => row.residual)) return null;
  const context = buildContextMap(request);
  const canAllocate = context.calibrated || (billing.inputEstimated && context.estimated);
  // An estimated input covers the locally visible context. A reported input must reconcile to the complete context.
  if (!canAllocate || context.total === 0 || context.total !== billing.input) return null;
  const cost = billing.rows.reduce((total, row) => total + (row.input != null ? row.cost : 0), 0);
  const withCost = row => ({ ...row, input: row.tokens, cost: cost * (row.tokens / context.total), estimated: true });
  const rows = context.rows.filter(row => row.tokens > 0).map(row => ({
    ...withCost(row),
    entries: row.entries?.filter(entry => entry.tokens > 0).map(withCost).sort((a, b) => b.tokens - a.tokens),
  }));
  return { cost, rows };
}

export function accumulateWaterfall(rows, mode, startAt = 0) {
  let running = startAt;
  const steps = rows.map(row => {
    const value = mode === 'tokens' ? row.tokens : row.cost;
    const start = running;
    running += value;
    return { ...row, start, end: running, value };
  });
  const scale = Math.max(1e-12, ...steps.flatMap(row => [row.start, row.end]));
  return { steps, scale };
}
