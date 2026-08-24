/* eslint-disable react-refresh/only-export-components */
import { Plus, Trash2 } from 'lucide-react';
import { Button, CustomSelect, SegmentedTabs, Tooltip } from '@components/ui';
import { formatMultiplierInputValue, parseMultiplierExpression } from '@utils/multiplier.js';

const MODE_OPTIONS = [
  { key: 'multiplier', label: '倍率' },
  { key: 'fixed', label: '固定单价' }
];

const SERVICE_TIER_OPTIONS = [
  { value: '', label: '全部档位' },
  { value: 'standard', label: 'Standard' },
  { value: 'batch', label: 'Batch' },
  { value: 'flex', label: 'Flex' },
  { value: 'fast', label: 'Fast mode' }
];

const CONTEXT_OPTIONS = [
  { value: '', label: '全部上下文' },
  { value: 'normal', label: '普通上下文' },
  { value: 'long', label: 'Long context' }
];

const MULTIPLIER_FIELDS = [
  { key: 'costMultiplier', label: '总倍率' },
  { key: 'inputCostMultiplier', label: '输入' },
  { key: 'outputCostMultiplier', label: '输出' },
  { key: 'cacheCreationCostMultiplier', label: '缓存写入' },
  { key: 'cacheReadCostMultiplier', label: '缓存读取' }
];

const FIXED_PRICE_FIELDS = [
  { key: 'inputPrice', label: '输入单价' },
  { key: 'outputPrice', label: '输出单价' },
  { key: 'cacheCreationPrice', label: '缓存写入单价' },
  { key: 'cacheReadPrice', label: '缓存读取单价' }
];

const read = (rule, camel, snake, fallback) => rule?.[camel] ?? rule?.[snake] ?? fallback;

export const createBillingRuleRow = (rule = {}) => ({
  id: rule.id || `billing-rule-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`,
  model: String(read(rule, 'model', 'model', '')),
  serviceTier: String(read(rule, 'serviceTier', 'service_tier', '')),
  context: String(read(rule, 'context', 'context', '')),
  mode: read(rule, 'mode', 'mode', 'multiplier') === 'fixed' ? 'fixed' : 'multiplier',
  costMultiplier: formatMultiplierInputValue(read(rule, 'costMultiplier', 'cost_multiplier', 1)),
  inputCostMultiplier: formatMultiplierInputValue(read(rule, 'inputCostMultiplier', 'input_cost_multiplier', 1)),
  outputCostMultiplier: formatMultiplierInputValue(read(rule, 'outputCostMultiplier', 'output_cost_multiplier', 1)),
  cacheCreationCostMultiplier: formatMultiplierInputValue(read(rule, 'cacheCreationCostMultiplier', 'cache_creation_cost_multiplier', 1)),
  cacheReadCostMultiplier: formatMultiplierInputValue(read(rule, 'cacheReadCostMultiplier', 'cache_read_cost_multiplier', 1)),
  inputPrice: String(read(rule, 'inputPrice', 'input_price', 0)),
  outputPrice: String(read(rule, 'outputPrice', 'output_price', 0)),
  cacheCreationPrice: String(read(rule, 'cacheCreationPrice', 'cache_creation_price', 0)),
  cacheReadPrice: String(read(rule, 'cacheReadPrice', 'cache_read_price', 0))
});

const isCatchAll = (rule) => !String(rule?.model || '').trim() && !rule?.serviceTier && !rule?.context;

export const buildInitialBillingRuleRows = (rules) => {
  if (!Array.isArray(rules) || rules.length === 0) return [];
  return [...rules]
    .sort((left, right) => Number(isCatchAll(right)) - Number(isCatchAll(left)))
    .map(createBillingRuleRow);
};

export const createDefaultBillingRuleRow = (multipliers = {}) => createBillingRuleRow({
  mode: 'multiplier',
  costMultiplier: multipliers.costMultiplier ?? 1,
  inputCostMultiplier: multipliers.inputCostMultiplier ?? 1,
  outputCostMultiplier: multipliers.outputCostMultiplier ?? 1,
  cacheCreationCostMultiplier: multipliers.cacheCreationCostMultiplier ?? 1,
  cacheReadCostMultiplier: multipliers.cacheReadCostMultiplier ?? 1
});

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
    const context = String(row?.context || '').trim();
    const selectorKey = JSON.stringify([model, serviceTier, context]);
    if (seen.has(selectorKey)) return { error: '组合计费规则的模型、档位和上下文不能重复' };
    seen.add(selectorKey);
    if (!model && !serviceTier && !context) catchAllCount += 1;

    const mode = row?.mode === 'fixed' ? 'fixed' : 'multiplier';
    const rule = { model, serviceTier, context, mode };
    const fields = mode === 'fixed' ? FIXED_PRICE_FIELDS : MULTIPLIER_FIELDS;
    for (const field of fields) {
      const value = parseMultiplierExpression(row?.[field.key], Number.NaN);
      const valid = Number.isFinite(value) && (mode === 'fixed' ? value >= 0 : value > 0);
      if (!valid) {
        return { error: `${field.label}必须是${mode === 'fixed' ? '大于或等于 0' : '大于 0'}的数字或分数` };
      }
      rule[field.key] = value;
    }
    payload.push(rule);
  }
  if (catchAllCount !== 1) return { error: '组合计费必须且只能包含一条默认规则' };
  return { payload };
};

const BillingRuleEditor = ({ rows, onChange, disabled = false }) => {
  const updateRow = (id, patch) => onChange?.(rows.map((row) => (row.id === id ? { ...row, ...patch } : row)));
  const removeRow = (id) => onChange?.(rows.filter((row) => row.id !== id));
  const addRule = () => onChange?.([...rows, createBillingRuleRow()]);

  return (
    <div className="space-y-3">
      {rows.map((row, index) => {
        const defaultRule = index === 0 && isCatchAll(row);
        const fields = row.mode === 'fixed' ? FIXED_PRICE_FIELDS : MULTIPLIER_FIELDS;
        return (
          <div key={row.id} className="rounded-lg border border-slate-200 bg-slate-50/60 p-3">
            <div className="flex items-center justify-between gap-3">
              <span className="text-xs font-semibold text-slate-700">{defaultRule ? '默认规则' : `例外规则 ${index}`}</span>
              <div className="flex items-center gap-2">
                <SegmentedTabs
                  tabs={MODE_OPTIONS}
                  activeTab={row.mode}
                  onChange={(mode) => updateRow(row.id, { mode })}
                  disabled={disabled}
                  className="w-44 grid-cols-2"
                  buttonPaddingClass="px-2"
                />
                {!defaultRule && (
                  <Tooltip content="删除规则">
                    <button
                      type="button"
                      onClick={() => removeRow(row.id)}
                      disabled={disabled}
                      className="inline-flex size-8 items-center justify-center rounded-lg text-slate-400 hover:bg-rose-50 hover:text-rose-600 disabled:cursor-not-allowed disabled:opacity-50"
                      aria-label="删除规则"
                    >
                      <Trash2 size={15} />
                    </button>
                  </Tooltip>
                )}
              </div>
            </div>

            {defaultRule ? (
              <div className="mt-3 flex flex-wrap gap-2 text-xs text-slate-500">
                <span>全部模型</span><span className="text-slate-300">/</span>
                <span>全部档位</span><span className="text-slate-300">/</span>
                <span>全部上下文</span>
              </div>
            ) : (
              <div className="mt-3 grid grid-cols-1 gap-2 sm:grid-cols-3">
                <input
                  type="text"
                  value={row.model}
                  onChange={(event) => updateRow(row.id, { model: event.target.value })}
                  placeholder="全部模型"
                  disabled={disabled}
                  className="min-w-0 rounded-lg border border-slate-200 bg-white px-2.5 py-2 text-sm focus:border-indigo-500 focus:outline-none focus:ring-2 focus:ring-indigo-500/20"
                />
                <CustomSelect options={SERVICE_TIER_OPTIONS} value={row.serviceTier} onChange={(serviceTier) => updateRow(row.id, { serviceTier })} disabled={disabled} size="md" fullWidth />
                <CustomSelect options={CONTEXT_OPTIONS} value={row.context} onChange={(context) => updateRow(row.id, { context })} disabled={disabled} size="md" fullWidth />
              </div>
            )}

            <div className={`mt-3 grid grid-cols-2 gap-2 ${fields.length === 5 ? 'sm:grid-cols-5' : 'sm:grid-cols-4'}`}>
              {fields.map((field) => (
                <label key={field.key} className="min-w-0">
                  <span className="mb-1 block truncate text-[11px] text-slate-500">{field.label}</span>
                  <input
                    type="text"
                    inputMode="decimal"
                    value={row[field.key]}
                    onChange={(event) => updateRow(row.id, { [field.key]: event.target.value })}
                    disabled={disabled}
                    className="w-full min-w-0 rounded-lg border border-slate-200 bg-white px-2.5 py-1.5 text-sm focus:border-indigo-500 focus:outline-none focus:ring-2 focus:ring-indigo-500/20"
                    placeholder={row.mode === 'fixed' ? '0' : '1.0'}
                  />
                </label>
              ))}
            </div>
          </div>
        );
      })}

      <Button type="button" variant="secondary" size="sm" onClick={addRule} disabled={disabled}>
        <Plus size={14} />
        添加例外规则
      </Button>
    </div>
  );
};

export default BillingRuleEditor;
