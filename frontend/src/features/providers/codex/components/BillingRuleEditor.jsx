/* eslint-disable react-refresh/only-export-components */
import { useState } from 'react';
import { Pencil, Plus, Trash2 } from 'lucide-react';
import { Button, CustomSelect, SegmentedTabs, Tooltip } from '@/shared/ui/index.js';
import MultiplierSettingsModal from '@/features/providers/shared/billing/MultiplierSettingsModal.jsx';
import { getCacheWritePolicyLabel } from '@/features/providers/shared/billing/multiplierPricing.js';
import { formatMultiplierValue } from '@/shared/lib/multiplier.js';
import {
  createBillingRuleRow,
  getBillingRuleEffectiveMultipliers,
  FIXED_PRICE_FIELDS,
  MULTIPLIER_FIELDS
} from '../utils/billingRuleRows.js';

export {
  buildInitialBillingRuleRows,
  createDefaultBillingRuleRow,
  parseBillingRuleRows
} from '../utils/billingRuleRows.js';

const MODE_OPTIONS = [
  { key: 'multiplier', label: '倍率' },
  { key: 'fixed', label: '固定单价' }
];

const SERVICE_TIER_OPTIONS = [
  { value: '', label: '全部档位' },
  { value: 'standard', label: 'Standard' },
  { value: 'batch', label: 'Batch' },
  { value: 'flex', label: 'Flex' },
  { value: 'fast', label: 'Fast' },
  { value: 'ultrafast', label: 'Ultrafast' }
];

const isCatchAll = (rule) => !String(rule?.model || '').trim()
  && !String(rule?.serviceTier || '').trim()
  && !String(rule?.baseContext || '').trim();

const longKey = (key) => `longContext${key[0].toUpperCase()}${key.slice(1)}`;

const PRICE_KEYS = {
  inputCostMultiplier: 'inputPrice',
  outputCostMultiplier: 'outputPrice',
  cacheCreationCostMultiplier: 'cacheWritePrice30m',
  cacheReadCostMultiplier: 'cacheReadPrice'
};

const LONG_CONTEXT_PRICE_KEYS = {
  inputCostMultiplier: 'longContextInputPrice',
  outputCostMultiplier: 'longContextOutputPrice',
  cacheCreationCostMultiplier: 'longContextCacheWritePrice',
  cacheReadCostMultiplier: 'longContextCacheReadPrice'
};

const getTierLabel = (row, index) => {
  if (isCatchAll(row)) return 'Long context';
  return SERVICE_TIER_OPTIONS.find((option) => option.value === row.serviceTier)?.label || `例外规则 ${index}`;
};

const getSummary = (row, fields, cacheWriteTTLPricing, prefix = '') => fields
  .map((field) => `${field.label}${field.label.includes('缓存写入') ? `（${getCacheWritePolicyLabel(cacheWriteTTLPricing)}）` : ''} ${row[prefix ? longKey(field.key) : field.key]}`)
  .join(' · ');

const getModalFields = (fields, prefix, mode) => fields.map((field) => ({
  ...field,
  key: prefix ? longKey(field.key) : field.key,
  multiplierKey: field.key.replace('Price', 'CostMultiplier'),
  ...(mode === 'multiplier' && field.key !== 'costMultiplier'
    ? { priceKey: (prefix ? LONG_CONTEXT_PRICE_KEYS : PRICE_KEYS)[field.key] }
    : {})
}));

const BillingRuleEditor = ({ rows, groupMultipliers = {}, longContextEnabled = true, cacheWriteTTLPricing = false, onChange, disabled = false }) => {
  const [editing, setEditing] = useState(null);
  const updateRow = (id, patch) => onChange?.(rows.map((row) => (row.id === id ? { ...row, ...patch } : row)));
  const removeRow = (id) => {
    if (editing?.rowId === id) setEditing(null);
    if (isCatchAll(rows.find((row) => row.id === id))) {
      updateRow(id, { longContextEnabled: false });
      return;
    }
    onChange?.(rows.filter((row) => row.id !== id));
  };
  const addRule = () => onChange?.([...rows, createBillingRuleRow({ context: 'normal' })]);
  const editingRow = rows.find((row) => row.id === editing?.rowId);
  const editingLongContext = editing?.context === 'long';
  const editingMode = editingLongContext ? editingRow?.longContextMode : editingRow?.mode;
  const editingFields = editingMode === 'fixed' ? FIXED_PRICE_FIELDS : MULTIPLIER_FIELDS;
  const editingIndex = editingRow ? rows.findIndex((row) => row.id === editingRow.id) : -1;
  const visibleRows = rows.filter((row) => !isCatchAll(row) || (longContextEnabled && row.longContextEnabled));

  const renderGroupRelationship = (row, longContext = false) => {
    const factors = getBillingRuleEffectiveMultipliers(row, groupMultipliers, longContext);
    return (
      <p className="mt-2 min-w-0 break-words text-[11px] leading-relaxed text-slate-500">
        {factors.fixed
          ? `固定单价 × 分组总倍率 ${formatMultiplierValue(factors.groupTotal, 6)}；各分项同时应用分组分项倍率。`
          : `分组 ${formatMultiplierValue(factors.groupTotal, 6)} × 规则 ${formatMultiplierValue(factors.ruleTotal, 6)} = 总倍率 ${formatMultiplierValue(factors.total, 6)}`}
      </p>
    );
  };

  return (
    <div className="space-y-3">
      {visibleRows.length === 0 && <p className="text-xs leading-relaxed text-slate-500">暂无附加规则，可按模型或服务档位添加。</p>}
      {visibleRows.map((row, index) => {
        const globalLongContext = isCatchAll(row);
        const fields = row.mode === 'fixed' ? FIXED_PRICE_FIELDS : MULTIPLIER_FIELDS;
        const longFields = row.longContextMode === 'fixed' ? FIXED_PRICE_FIELDS : MULTIPLIER_FIELDS;
        const summary = getSummary(row, fields, cacheWriteTTLPricing);
        const longSummary = getSummary(row, longFields, cacheWriteTTLPricing, 'longContext');
        return (
          <div key={row.id} className="rounded-lg border border-slate-200 bg-slate-50/60 p-3">
            <div className="flex items-center justify-between gap-3">
              <div className="flex min-w-0 items-center gap-2">
                <span className="truncate text-sm font-semibold text-slate-700">{getTierLabel(row, index)}</span>
              </div>
              <Tooltip content="删除规则">
                <button
                  type="button"
                  onClick={() => removeRow(row.id)}
                  disabled={disabled}
                  className="inline-flex size-8 shrink-0 items-center justify-center rounded-lg text-slate-400 hover:bg-rose-50 hover:text-rose-600 disabled:cursor-not-allowed disabled:opacity-50"
                  aria-label="删除规则"
                >
                  <Trash2 size={15} />
                </button>
              </Tooltip>
            </div>

            {globalLongContext ? (
              <div className="mt-1 text-xs text-slate-500">全部模型 · 全部档位 · Long context</div>
            ) : (
              <div className="mt-2 grid grid-cols-1 gap-2 sm:grid-cols-2">
                <input
                  type="text"
                  value={row.model}
                  onChange={(event) => updateRow(row.id, { model: event.target.value })}
                  placeholder="全部模型"
                  disabled={disabled}
                  className="min-w-0 rounded-lg border border-slate-200 bg-white px-2.5 py-2 text-sm focus:border-indigo-500 focus:outline-none focus:ring-2 focus:ring-indigo-500/20"
                />
                <CustomSelect options={SERVICE_TIER_OPTIONS} value={row.serviceTier} onChange={(serviceTier) => updateRow(row.id, { serviceTier })} disabled={disabled} size="md" fullWidth />
              </div>
            )}

            {!globalLongContext && (
              <>
                <div className="mt-2 flex flex-wrap items-center gap-2 border-t border-slate-200 pt-2">
                  <div className="min-w-0 flex-1">
                    <div className="text-xs font-medium text-slate-700">普通上下文</div>
                    <div className="break-words text-[11px] text-slate-500">{summary}</div>
                  </div>
                  <SegmentedTabs
                    tabs={MODE_OPTIONS}
                    activeTab={row.mode}
                    onChange={(mode) => updateRow(row.id, { mode })}
                    disabled={disabled}
                    className="w-36 grid-cols-2"
                    buttonPaddingClass="px-1.5"
                  />
                  <Button type="button" variant="secondary" size="sm" icon={Pencil} onClick={() => setEditing({ rowId: row.id, context: 'normal' })} disabled={disabled}>编辑</Button>
                </div>
                {renderGroupRelationship(row)}
              </>
            )}

            {longContextEnabled && <div className="mt-2 flex flex-wrap items-center gap-2 border-t border-slate-200 pt-2">
              <button
                type="button"
                role="switch"
                aria-label={`${getTierLabel(row, index)} Long context 规则`}
                aria-checked={row.longContextEnabled}
                onClick={() => updateRow(row.id, { longContextEnabled: !row.longContextEnabled })}
                disabled={disabled}
                className="flex min-w-36 items-center gap-2 text-left disabled:cursor-not-allowed disabled:opacity-50"
              >
                <span className={`relative inline-flex h-5 w-9 shrink-0 rounded-full transition-colors ${row.longContextEnabled ? 'bg-indigo-600' : 'bg-slate-300'}`}>
                  <span className={`absolute top-0.5 size-4 rounded-full bg-white shadow transition-transform ${row.longContextEnabled ? 'translate-x-[1.125rem]' : 'translate-x-0.5'}`} />
                </span>
                <span className="text-xs font-medium text-slate-700">Long context</span>
              </button>

              {row.longContextEnabled && (
                <>
                  <div className="order-last basis-full min-w-0 break-words text-[11px] text-slate-500">{longSummary}</div>
                  <SegmentedTabs
                    tabs={MODE_OPTIONS}
                    activeTab={row.longContextMode}
                    onChange={(longContextMode) => updateRow(row.id, { longContextMode })}
                    disabled={disabled}
                    className="ml-auto w-36 grid-cols-2"
                    buttonPaddingClass="px-1.5"
                  />
                  <Button type="button" variant="secondary" size="sm" icon={Pencil} onClick={() => setEditing({ rowId: row.id, context: 'long' })} disabled={disabled}>编辑</Button>
                </>
              )}
            </div>}
            {longContextEnabled && row.longContextEnabled && renderGroupRelationship(row, true)}
          </div>
        );
      })}

      <Button type="button" variant="secondary" size="sm" onClick={addRule} disabled={disabled}>
        <Plus size={14} />
        添加例外规则
      </Button>

      {editingRow && (
        <MultiplierSettingsModal
          title={`${getTierLabel(editingRow, editingIndex)} · ${editingLongContext ? 'Long context' : '普通上下文'}${editingMode === 'fixed' ? '固定单价' : '倍率'}`}
          description="预览已包含分组倍率。"
          fields={getModalFields(editingFields, editingLongContext ? 'longContext' : '', editingMode)}
          values={editingRow}
          inheritedMultipliers={groupMultipliers}
          pricingServiceTier={editingRow.serviceTier}
          pricingModelName={editingRow.model}
          cacheWriteTTLPricing={cacheWriteTTLPricing}
          showPricingReference={editingMode === 'multiplier'}
          placeholder={editingMode === 'fixed' ? '0' : '1.0'}
          disabled={disabled}
          onCancel={() => setEditing(null)}
          onSave={(values) => {
            updateRow(editingRow.id, values);
            setEditing(null);
          }}
        />
      )}
    </div>
  );
};

export default BillingRuleEditor;
