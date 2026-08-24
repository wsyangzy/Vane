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
  auto: ['Auto', '请求让上游自动决定处理档位；仅凭请求无法确定最终计费档位。', 'bg-slate-50 text-slate-600 border-slate-200'],
  standard: ['Standard', '标准处理档位。', 'bg-indigo-50 text-indigo-700 border-indigo-200'],
  batch: ['Batch', '批处理定价档位；实际使用方式与可用性由上游决定。', 'bg-cyan-50 text-cyan-700 border-cyan-200'],
  flex: ['Flex', '弹性处理档位，通常以更慢响应和可能的资源暂不可用换取更低价格。', 'bg-emerald-50 text-emerald-700 border-emerald-200'],
  fast: ['Fast mode', '快速或优先处理档位；priority 作为同义值归入此档。', 'bg-amber-50 text-amber-700 border-amber-200']
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
