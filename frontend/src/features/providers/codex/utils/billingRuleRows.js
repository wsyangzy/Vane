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
const savedGroupMultiplierMode = (rule) => {
  if (!isCatchAllRule(rule)) return 'inherit';
  const explicit = read(rule, 'groupMultiplierMode', 'group_multiplier_mode', '');
  if (explicit === 'inherit' || explicit === 'override') return explicit;
  return isCatchAllRule(rule) ? 'override' : 'inherit';
};

export const getBillingRuleEffectiveMultipliers = (row, group = {}, longContext = false) => {
  const fieldKey = (key) => longContext ? longKey(key) : key;
  const inherit = longContext || !isCatchAllRule({ ...row, context: row?.baseContext })
    || row?.groupMultiplierMode !== 'override';
  const fixed = row?.[fieldKey('mode')] === 'fixed';
  const positive = (value) => {
    const parsed = parseMultiplierExpression(value, Number.NaN);
    return Number.isFinite(parsed) && parsed > 0 ? parsed : 1;
  };
  const groupTotal = inherit ? positive(group.costMultiplier) : 1;
  const ruleTotal = fixed ? 1 : positive(row?.[fieldKey('costMultiplier')]);
  const factors = Object.fromEntries(MULTIPLIER_FIELDS.slice(1).map(({ key }) => [key,
    (inherit ? positive(group[key]) : 1) * (fixed ? 1 : positive(row?.[fieldKey(key)]))
  ]));
  return { inherit, fixed, groupTotal, ruleTotal, total: groupTotal * ruleTotal, ...factors };
};
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
  groupMultiplierMode: isCatchAllRule(rule) ? read(rule, 'groupMultiplierMode', 'group_multiplier_mode', 'inherit') : 'inherit',
  ...readRuleFields(rule),
  longContextEnabled: Boolean(longRule),
  longContextMode: read(longRule, 'mode', 'mode', read(rule, 'mode', 'mode', 'multiplier')) === 'fixed' ? 'fixed' : 'multiplier',
  longContextGroupMultiplierMode: 'inherit',
  ...readRuleFields(longRule, 'longContext')
});

const isCatchAllRule = (rule) => !String(read(rule, 'model', 'model', '')).trim()
  && !String(read(rule, 'serviceTier', 'service_tier', '')).trim()
  && !String(read(rule, 'context', 'context', '')).trim();

export const buildInitialBillingRuleRows = (rules, group = null) => {
  if (!Array.isArray(rules) || rules.length === 0) return [];

  const longRules = new Map();
  const baseRules = [];
  for (const savedRule of rules) {
    // Model/tier/context rules always inherit, including older override records.
    // The old catch-all already contains the group rate and must not double it.
    let rule = { ...savedRule, groupMultiplierMode: savedGroupMultiplierMode(savedRule) };
    // A legacy default copied the group factors. Keep one source of those
    // factors when the hidden default and the group still describe the same rate.
    if (group && isCatchAllRule(rule) && rule.mode !== 'fixed' && rule.groupMultiplierMode === 'override'
      && MULTIPLIER_FIELDS.every(({ key }) => {
        const snake = key.replace(/[A-Z]/g, (letter) => `_${letter.toLowerCase()}`);
        return parseMultiplierExpression(read(rule, key, snake, 1), 1) === parseMultiplierExpression(read(group, key, snake, 1), 1);
      })) {
      rule = { ...rule, groupMultiplierMode: 'inherit', ...Object.fromEntries(MULTIPLIER_FIELDS.map(({ key }) => [key, 1])) };
    }
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
    mode,
    groupMultiplierMode: !context && !String(row?.model || '').trim() && !String(row?.serviceTier || '').trim()
      && row?.groupMultiplierMode === 'override' ? 'override' : 'inherit'
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
    if (!model && !serviceTier && row?.baseContext === 'normal') {
      return { error: '请为附加规则指定模型或服务档位' };
    }
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
