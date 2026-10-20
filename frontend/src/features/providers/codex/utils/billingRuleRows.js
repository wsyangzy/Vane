import { formatMultiplierInputValue, parseMultiplierExpression } from '../../../../shared/lib/multiplier.js';

export const MULTIPLIER_FIELDS = [
  { key: 'costMultiplier', label: '总倍率' },
  { key: 'inputCostMultiplier', label: '输入' },
  { key: 'outputCostMultiplier', label: '输出' },
  { key: 'cacheCreationCostMultiplier', label: '缓存写入' },
  { key: 'cacheReadCostMultiplier', label: '缓存读取' }
];

export const FIXED_PRICE_FIELDS = [
  { key: 'inputPrice', label: '输入单价' },
  { key: 'outputPrice', label: '输出单价' },
  { key: 'cacheCreationPrice', label: '缓存写入单价' },
  { key: 'cacheReadPrice', label: '缓存读取单价' }
];

const read = (rule, camel, snake, fallback) => rule?.[camel] ?? rule?.[snake] ?? fallback;
const longKey = (key) => `longContext${key[0].toUpperCase()}${key.slice(1)}`;
const selectorKey = (rule) => JSON.stringify([
  String(read(rule, 'model', 'model', '')).trim(),
  String(read(rule, 'serviceTier', 'service_tier', '')).trim()
]);

const readRuleFields = (rule, prefix = '') => {
  const values = {};
  for (const field of MULTIPLIER_FIELDS) {
    values[prefix ? longKey(field.key) : field.key] = formatMultiplierInputValue(
      read(rule, field.key, field.key.replace(/[A-Z]/g, (letter) => `_${letter.toLowerCase()}`), 1)
    );
  }
  for (const field of FIXED_PRICE_FIELDS) {
    values[prefix ? longKey(field.key) : field.key] = String(
      read(rule, field.key, field.key.replace(/[A-Z]/g, (letter) => `_${letter.toLowerCase()}`), 0)
    );
  }
  return values;
};

export const createBillingRuleRow = (rule = {}, longRule = null) => ({
  id: rule.id || `billing-rule-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`,
  model: String(read(rule, 'model', 'model', '')),
  serviceTier: String(read(rule, 'serviceTier', 'service_tier', '')),
  baseContext: String(read(rule, 'context', 'context', '')),
  mode: read(rule, 'mode', 'mode', 'multiplier') === 'fixed' ? 'fixed' : 'multiplier',
  ...readRuleFields(rule),
  longContextEnabled: Boolean(longRule),
  longContextMode: read(longRule, 'mode', 'mode', read(rule, 'mode', 'mode', 'multiplier')) === 'fixed' ? 'fixed' : 'multiplier',
  ...readRuleFields(longRule, 'longContext')
});

const isCatchAllRule = (rule) => !String(read(rule, 'model', 'model', '')).trim()
  && !String(read(rule, 'serviceTier', 'service_tier', '')).trim()
  && !String(read(rule, 'context', 'context', '')).trim();

export const buildInitialBillingRuleRows = (rules) => {
  if (!Array.isArray(rules) || rules.length === 0) return [];

  const longRules = new Map();
  const baseRules = [];
  for (const rule of rules) {
    if (String(read(rule, 'context', 'context', '')).trim() === 'long') {
      longRules.set(selectorKey(rule), rule);
    } else {
      baseRules.push(rule);
    }
  }

  const pairedLongRules = new Set();
  const rows = baseRules
    .sort((left, right) => Number(isCatchAllRule(right)) - Number(isCatchAllRule(left)))
    .map((rule) => {
      const key = selectorKey(rule);
      const longRule = pairedLongRules.has(key) ? null : longRules.get(key) || null;
      if (longRule) pairedLongRules.add(key);
      return createBillingRuleRow(rule, longRule);
    });

  for (const [key, longRule] of longRules) {
    if (!pairedLongRules.has(key)) {
      const hasSelector = String(read(longRule, 'model', 'model', '')).trim()
        || String(read(longRule, 'serviceTier', 'service_tier', '')).trim();
      rows.push(createBillingRuleRow({ ...longRule, context: hasSelector ? 'normal' : '' }, longRule));
    }
  }
  return rows;
};

export const createDefaultBillingRuleRow = (multipliers = {}) => createBillingRuleRow({
  mode: 'multiplier',
  costMultiplier: multipliers.costMultiplier ?? 1,
  inputCostMultiplier: multipliers.inputCostMultiplier ?? 1,
  outputCostMultiplier: multipliers.outputCostMultiplier ?? 1,
  cacheCreationCostMultiplier: multipliers.cacheCreationCostMultiplier ?? 1,
  cacheReadCostMultiplier: multipliers.cacheReadCostMultiplier ?? 1
});

const parseRule = (row, mode, context, prefix = '') => {
  const rule = {
    model: String(row?.model || '').trim(),
    serviceTier: String(row?.serviceTier || '').trim(),
    context,
    mode
  };
  const fields = mode === 'fixed' ? FIXED_PRICE_FIELDS : MULTIPLIER_FIELDS;
  for (const field of fields) {
    const value = parseMultiplierExpression(row?.[prefix ? longKey(field.key) : field.key], Number.NaN);
    const valid = Number.isFinite(value) && (mode === 'fixed' ? value >= 0 : value > 0);
    if (!valid) {
      const scope = prefix ? 'Long context ' : '';
      return { error: `${scope}${field.label}必须是${mode === 'fixed' ? '大于或等于 0' : '大于 0'}的数字或分数` };
    }
    rule[field.key] = value;
  }
  return { rule };
};

export const parseBillingRuleRows = (rows) => {
  if (!Array.isArray(rows) || rows.length === 0) {
    return { error: '组合计费至少需要一条默认规则' };
  }

  const seen = new Set();
  let catchAllCount = 0;
  const payload = [];
  for (const row of rows) {
    const model = String(row?.model || '').trim();
    const serviceTier = String(row?.serviceTier || '').trim();
    const baseContext = !model && !serviceTier
      ? ''
      : (row?.baseContext === '' ? '' : 'normal');
    const baseSelector = JSON.stringify([model, serviceTier, baseContext]);
    if (seen.has(baseSelector)) return { error: '组合计费规则的模型和档位不能重复' };
    seen.add(baseSelector);
    if (!model && !serviceTier && !baseContext) catchAllCount += 1;

    const baseResult = parseRule(row, row?.mode === 'fixed' ? 'fixed' : 'multiplier', baseContext);
    if (baseResult.error) return baseResult;
    payload.push(baseResult.rule);

    if (row?.longContextEnabled) {
      const longSelector = JSON.stringify([model, serviceTier, 'long']);
      if (seen.has(longSelector)) return { error: '组合计费规则的模型和档位不能重复' };
      seen.add(longSelector);
      const longResult = parseRule(
        row,
        row?.longContextMode === 'fixed' ? 'fixed' : 'multiplier',
        'long',
        'longContext'
      );
      if (longResult.error) return longResult;
      payload.push(longResult.rule);
    }
  }

  if (catchAllCount !== 1) return { error: '组合计费必须且只能包含一条默认规则' };
  return { payload };
};
