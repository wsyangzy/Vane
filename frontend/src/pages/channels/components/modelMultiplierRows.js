import { formatMultiplierInputValue, isValidMultiplierExpression, parseMultiplierExpression } from '@utils/multiplier.js';

export const MODEL_MULTIPLIER_FIELDS = [
  { key: 'costMultiplier', label: '总倍率' },
  { key: 'inputCostMultiplier', label: '输入' },
  { key: 'outputCostMultiplier', label: '输出' },
  { key: 'cacheReadCostMultiplier', label: '缓存读取' }
];

export const createModelMultiplierRow = (rule = {}) => ({
  id: rule.id || `model-multiplier-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`,
  model: String(rule.model || ''),
  costMultiplier: formatMultiplierInputValue(rule.costMultiplier ?? 1.0),
  inputCostMultiplier: formatMultiplierInputValue(rule.inputCostMultiplier ?? 1.0),
  outputCostMultiplier: formatMultiplierInputValue(rule.outputCostMultiplier ?? 1.0),
  cacheReadCostMultiplier: formatMultiplierInputValue(rule.cacheReadCostMultiplier ?? 1.0)
});

export const buildInitialModelMultiplierRows = (rules) => (
  Array.isArray(rules)
    ? rules
      .map((rule) => createModelMultiplierRow(rule))
      .filter((row) => row.model.trim() || MODEL_MULTIPLIER_FIELDS.some((field) => row[field.key] !== '1'))
    : []
);

export const parseModelMultiplierRows = (rows) => {
  const seen = new Set();
  const payload = [];
  for (const row of rows || []) {
    const model = String(row?.model || '').trim();
    const hasNonDefaultMultiplier = MODEL_MULTIPLIER_FIELDS.some((field) => {
      const text = String(row?.[field.key] ?? '').trim();
      if (!text) return false;
      const parsed = parseMultiplierExpression(text, Number.NaN);
      return !Number.isFinite(parsed) || Math.abs(parsed - 1) > 0.000001;
    });
    if (!model && !hasNonDefaultMultiplier) {
      continue;
    }
    if (!model) {
      return { error: '模型附加倍率的模型名称不能为空' };
    }
    if (seen.has(model)) {
      return { error: `模型附加倍率重复：${model}` };
    }
    seen.add(model);

    const rule = { model };
    for (const field of MODEL_MULTIPLIER_FIELDS) {
      if (!isValidMultiplierExpression(row?.[field.key])) {
        return { error: `${model} 的${field.label}必须是非负数字或分数，例如 1.25 或 40/30` };
      }
      rule[field.key] = parseMultiplierExpression(row?.[field.key], 1.0);
    }
    payload.push(rule);
  }
  return { payload };
};
