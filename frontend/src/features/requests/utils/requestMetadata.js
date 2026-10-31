export const REASONING_FIELD_LABELS = {
  effort: '推理强度',
  mode: '推理模式',
  summary: '推理摘要',
  context: '上下文窗口',
  type: '扩展思考',
  budget_tokens: 'Token 预算'
};

export const REASONING_VALUE_LABELS = {
  mode: { standard: 'standard · 标准推理', pro: 'pro · 深度推理' },
  summary: { auto: 'auto · 自动', concise: 'concise · 简洁摘要', detailed: 'detailed · 详细摘要' },
  context: { auto: 'auto · 自动', current_turn: 'current_turn · 仅当前轮次', all_turns: 'all_turns · 全部对话轮次' },
  type: {
    enabled: 'enabled · 已开启',
    disabled: 'disabled · 已关闭',
    adaptive: 'adaptive · 自适应'
  }
};

// Anthropic extended thinking 的 type 值域为 enabled / adaptive / disabled。
// adaptive 属于开启态（由模型自行决定思考量），不能与 disabled 共用关闭态样式。
const ACTIVE_THINKING_TYPES = new Set(['enabled', 'adaptive']);

export const isThinkingTypeActive = (value) => (
  ACTIVE_THINKING_TYPES.has(String(value || '').trim().toLowerCase())
);

const REASONING_EFFORTS = {
  none: ['none', '不请求额外推理；是否支持取决于模型。', 'bg-slate-50 text-slate-600 border-slate-200'],
  minimal: ['minimal', '采用最少的推理投入，优先降低延迟与成本。', 'bg-slate-100 text-slate-700 border-slate-300'],
  low: ['low', '采用较低推理投入，适合简单任务。', 'bg-blue-50 text-blue-700 border-blue-200'],
  medium: ['medium', '采用中等推理投入，平衡质量、延迟与成本。', 'bg-emerald-50 text-emerald-700 border-emerald-200'],
  high: ['high', '采用较高推理投入，适合复杂任务。', 'bg-amber-50 text-amber-700 border-amber-200'],
  xhigh: ['xhigh', '采用极高推理投入；仅部分模型支持。', 'bg-orange-50 text-orange-700 border-orange-200'],
  max: ['max', 'Codex 产品的延长执行模式，不是标准单请求 reasoning.effort。', 'bg-rose-50 text-rose-700 border-rose-200'],
  ultra: ['ultra', 'Codex 产品的多代理编排模式，不是标准单请求 reasoning.effort。', 'bg-violet-50 text-violet-700 border-violet-200']
};

const SERVICE_TIERS = {
  ultrafast: ['Ultrafast', '超快速处理档位，使用独立的 Ultrafast 单价。', 'bg-rose-50 text-rose-700 border-rose-200'],
  auto: ['Auto', '请求让上游自动决定处理档位；仅凭请求无法确定最终计费档位。', 'bg-slate-50 text-slate-600 border-slate-200'],
  standard: ['Standard', '标准处理档位。', 'bg-indigo-50 text-indigo-700 border-indigo-200'],
  batch: ['Batch', '批处理定价档位；实际使用方式与可用性由上游决定。', 'bg-cyan-50 text-cyan-700 border-cyan-200'],
  flex: ['Flex', '弹性处理档位，通常以更慢响应和可能的资源暂不可用换取更低价格。', 'bg-emerald-50 text-emerald-700 border-emerald-200'],
  fast: ['Fast', '快速或优先处理档位；priority 作为同义值归入此档。', 'bg-amber-50 text-amber-700 border-amber-200']
};

export const parseReasoningConfig = (raw) => {
  if (!raw) return {};
  if (typeof raw === 'object' && !Array.isArray(raw)) return raw;
  if (typeof raw !== 'string') return {};
  const text = raw.trim();
  if (!text) return {};
  try {
    const parsed = JSON.parse(text);
    if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) return parsed;
    if (typeof parsed === 'string' && parsed.trim()) return { effort: parsed.trim() };
    return {};
  } catch {
    return { effort: text };
  }
};

export const getReasoningEffortMeta = (value) => {
  const effort = String(value || '').trim().toLowerCase();
  const configured = REASONING_EFFORTS[effort];
  if (configured) return { value: effort, label: configured[0], description: configured[1], className: configured[2] };
  return {
    value: effort,
    label: effort || '-',
    description: effort ? '上游返回了当前界面未识别的推理强度，保留原值用于排障。' : '',
    className: 'bg-slate-50 text-slate-600 border-slate-200'
  };
};

export const getServiceTierMeta = (value) => {
  const raw = String(value || '').trim().toLowerCase();
  const tier = raw === 'priority' ? 'fast' : (raw === 'default' || raw === 'standard_only' ? 'standard' : raw);
  const configured = SERVICE_TIERS[tier];
  if (configured) return { value: tier, raw, label: configured[0], description: configured[1], className: configured[2] };
  return raw ? {
    value: raw,
    raw,
    label: raw,
    description: '上游返回了当前界面未识别的服务档位，计费不会猜测对应配置。',
    className: 'bg-slate-50 text-slate-600 border-slate-200'
  } : null;
};

export const getRequestServiceTierMeta = (request) => {
  const snapshot = request.billingRuleSnapshot;
  // 使用成本计算时记录的档位；命中标准兜底价格不代表请求降为 Standard。
  const tier = snapshot?.pricing_provider === 'codex' && snapshot.service_tier_source
    ? snapshot.billing_service_tier || 'standard'
    : request.serviceTier || request.service_tier || '';
  return getServiceTierMeta(tier);
};

export const getRequestBillingMultiplierSources = (request) => {
  const positive = (value) => Number.isFinite(Number(value)) && Number(value) > 0 ? Number(value) : 1;
  const snapshot = request.billingRuleSnapshot;
  const hasSnapshot = Boolean(snapshot?.context_mode);
  const hasRuleSources = hasSnapshot && Boolean(snapshot.rule_key && snapshot.rule_group_multiplier_mode);
  const groupApplied = !hasRuleSources || snapshot.rule_group_multiplier_mode !== 'override';
  const group = positive(hasSnapshot ? snapshot.group_cost_multiplier : request.costMultiplier);
  const model = positive(hasSnapshot ? snapshot.model_cost_multiplier : request.modelCostMultiplier);
  const rule = hasRuleSources ? positive(snapshot.rule_cost_multiplier) : 1;
  return {
    group, model, rule, hasRuleSources, groupApplied,
    ruleGroupMode: hasRuleSources ? snapshot.rule_group_multiplier_mode : '',
    total: hasSnapshot ? positive(snapshot.total_cost_multiplier ?? group * model * rule) : group * model
  };
};

// 请求终态（request_logs.stream_outcome）。它回答的是"客户端拿到完整结果了吗"，
// 与生命周期状态不同：流式请求缺结束事件时状态仍是 completed，终态才是 incomplete。
const STREAM_OUTCOMES = {
  completed: ['完整', '流式响应收到了结束事件，客户端拿到了完整结果。', 'bg-emerald-50 text-emerald-700 border-emerald-200'],
  not_streaming: ['非流式完成', '非流式请求已返回结果。', 'bg-emerald-50 text-emerald-700 border-emerald-200'],
  incomplete: ['不完整', '上游返回 2xx，但流缺少结束事件或中途截断，客户端拿到的可能是半截回答。', 'bg-amber-50 text-amber-700 border-amber-200'],
  failed: ['失败', '请求最终失败。', 'bg-rose-50 text-rose-700 border-rose-200'],
  canceled: ['已取消', '客户端主动断开。', 'bg-slate-100 text-slate-600 border-slate-200']
};

// 历史行与进行中的请求没有终态，返回 null 由展示层显示 "-"
export const getStreamOutcomeMeta = (value) => {
  const outcome = String(value || '').trim().toLowerCase();
  if (!outcome) return null;
  const configured = STREAM_OUTCOMES[outcome];
  if (configured) return { value: outcome, label: configured[0], description: configured[1], className: configured[2] };
  return { value: outcome, label: outcome, description: '当前界面未识别的终态，保留原值用于排障。', className: 'bg-slate-50 text-slate-600 border-slate-200' };
};

export const formatReasoningFieldValue = (key, value) => {
  if (typeof value === 'number') return value.toLocaleString();
  if (typeof value === 'string') return REASONING_VALUE_LABELS[key]?.[value] || value;
  if (typeof value === 'boolean') return value ? 'true · 是' : 'false · 否';
  try {
    return JSON.stringify(value);
  } catch {
    return String(value);
  }
};

// ============================================
// 隐私防护
// 摘要只含规则名与命中数。命中原文不落库、不回传，此处永远拿不到原文。
// ============================================

export const PRIVACY_RULE_SOURCE_LABELS = {
  builtin: '内置',
  preset: '预设',
  custom: '自定义'
};

const normalizePrivacySummary = (value) => ({
  changed: Boolean(value?.changed),
  action: typeof value?.action === 'string' ? value.action : '',
  hitCount: Number(value?.hit_count ?? 0),
  hits: Array.isArray(value?.hits) ? value.hits : [],
  activity: value?.activity && typeof value.activity === 'object' && !Array.isArray(value.activity) ? value.activity : null
});

export const parsePrivacySummary = (value) => {
  const empty = { changed: false, action: '', hitCount: 0, hits: [] };
  if (!value) return empty;
  if (typeof value === 'object') return normalizePrivacySummary(value);
  if (typeof value !== 'string') return empty;
  try {
    return normalizePrivacySummary(JSON.parse(value));
  } catch {
    return empty;
  }
};

/**
 * 计算隐私防护的展示状态。
 *
 * 刻意区分「已脱敏」与「仅检测到」：detect 模式下命中会被统计但请求原样发出，
 * 若两者共用同一个绿色徽章，用户会误以为内容已经被保护。
 */
export const getPrivacyDisplay = (request) => {
  const summary = parsePrivacySummary(
    request?.privacyRedactionSummary || request?.privacy_redaction_summary
  );
  const flagged = Boolean(request?.privacyRedacted ?? request?.privacy_redacted);
  const hitCount = summary.hitCount || summary.hits.reduce((sum, hit) => sum + (hit.count || 0), 0);

  if (summary.activity) {
    const activity = summary.activity;
    const state = Object.hasOwn(PRIVACY_ACTIVITY_LABELS, activity.state) ? activity.state : 'unknown';
    return {
      state, label: PRIVACY_ACTIVITY_LABELS[state],
      hitCount: Math.max(0, Number(activity.hit_count) || 0),
      hits: Array.isArray(activity.rule_hits) ? activity.rule_hits : [],
      detectOnly: state === 'detected', activity
    };
  }

  if (!flagged && hitCount === 0) {
    return { state: 'none', label: '无脱敏摘要', hitCount: 0, hits: [], detectOnly: false };
  }
  const detectOnly = summary.action === 'detect' || !summary.changed;
  return {
    state: detectOnly ? 'detected' : 'redacted',
    label: detectOnly ? '仅检测到' : '已脱敏',
    hitCount,
    hits: summary.hits,
    detectOnly
  };
};

export const PRIVACY_ACTIVITY_LABELS = {
  redacted: '已脱敏', detected: '仅检测到', blocked: '已拒发', clean: '扫描无命中',
  disabled: '防护关闭', skipped: '未完整扫描', truncated: '扫描已截断', unknown: '无扫描记录'
};

export const PRIVACY_STATE_STYLES = {
  redacted: 'border-emerald-200 bg-emerald-50 text-emerald-700',
  detected: 'border-amber-200 bg-amber-50 text-amber-700',
  blocked: 'border-rose-200 bg-rose-50 text-rose-700',
  clean: 'border-emerald-200 bg-emerald-50 text-emerald-700',
  disabled: 'border-slate-200 bg-slate-100 text-slate-600',
  skipped: 'border-amber-200 bg-amber-50 text-amber-700',
  truncated: 'border-amber-200 bg-amber-50 text-amber-700',
  unknown: 'border-slate-200 bg-slate-50 text-slate-500',
  none: 'border-slate-200 bg-slate-50 text-slate-400'
};
