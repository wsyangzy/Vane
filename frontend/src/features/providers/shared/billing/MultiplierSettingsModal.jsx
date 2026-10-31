import { useEffect, useId, useState } from 'react';
import { ArrowRight, CircleDollarSign, XCircle } from 'lucide-react';
import { Button, CustomSelect } from '@/shared/ui/index.js';
import { renderModalPortal } from '@/shared/ui/modalPortal.js';
import { getModelPricingsByProvider } from '@/services/wails/index.js';
import { parseMultiplierExpression } from '@/shared/lib/multiplier.js';
import { filterPricingsByTier, getCacheWritePolicyLabel, getMultiplierPriceReferences, normalizePricingTier } from './multiplierPricing.js';

const getFieldKey = (field) => field.key || field.name;

// 倍率是相对量，脱离基础单价无从判断填得对不对。这里展示所选模型的基础单价，
// 以及按 tracker.go CalculateCost 的口径算出的实际单价（单价 × 分项倍率 × 总倍率）。
const formatPrice = (value) => {
  const num = Number(value);
  if (!Number.isFinite(num) || num <= 0) return null;
  return `$${Number(num.toFixed(4))}`;
};

const pricingTierLabel = (value) => ({
  '': 'Standard',
  batch: 'Batch',
  flex: 'Flex',
  fast: 'Fast', ultrafast: 'Ultrafast'
}[normalizePricingTier(value)] || String(value || 'Standard'));

const pricingKey = (item) => JSON.stringify([
  item?.modelName || '',
  normalizePricingTier(item?.serviceTier)
]);

const MultiplierSettingsModal = ({
  title,
  description,
  fields,
  values,
  pricingProvider = 'codex',
  pricingServiceTier,
  pricingModelName,
  cacheWriteTTLPricing = false,
  inheritedMultipliers,
  showPricingReference = true,
  placeholder = '1.0',
  disabled = false,
  onCancel,
  onSave
}) => {
  const titleId = useId();
  const [draft, setDraft] = useState(() => ({ ...values }));
  const [error, setError] = useState('');
  const [pricings, setPricings] = useState([]);
  const [pricingModel, setPricingModel] = useState('');

  useEffect(() => {
    if (!showPricingReference) return undefined;
    let cancelled = false;
    getModelPricingsByProvider(pricingProvider)
      .then((records) => {
        if (cancelled) return;
        const loadedPricings = records || [];
        const visiblePricings = filterPricingsByTier(loadedPricings, pricingServiceTier);
        setPricings(loadedPricings);
        const defaultPricing = visiblePricings.find((item) => item.modelName === pricingModelName)
          || visiblePricings.find((item) => item.isDefault) || visiblePricings[0];
        setPricingModel(defaultPricing ? pricingKey(defaultPricing) : '');
      })
      // 定价只是填写时的对照参考，取不到不该挡住倍率配置
      .catch(() => {});
    return () => { cancelled = true; };
  }, [pricingProvider, pricingServiceTier, pricingModelName, showPricingReference]);

  const visiblePricings = filterPricingsByTier(pricings, pricingServiceTier);
  const selectedPricing = visiblePricings.find((item) => pricingKey(item) === pricingModel) || null;

  // 非法输入按 1.0 预览，校验交给保存时的 handleSave，避免边打字边报错
  const resolveMultiplier = (key) => {
    const parsed = parseMultiplierExpression(draft[key], Number.NaN);
    return Number.isFinite(parsed) && parsed >= 0 ? parsed : 1;
  };

  const totalField = fields.find((field) => /^(longContextCostMultiplier|costMultiplier)$/.test(getFieldKey(field)));
  const componentFields = fields.filter((field) => field !== totalField);
  const totalMultiplier = totalField ? resolveMultiplier(getFieldKey(totalField)) : 1;
  const inheritedMultiplier = (key) => {
    const value = parseMultiplierExpression(inheritedMultipliers?.[key], Number.NaN);
    return Number.isFinite(value) && value > 0 ? value : 1;
  };
  const groupTotalMultiplier = inheritedMultiplier('costMultiplier');
  const standardOnly = String(pricingServiceTier ?? '').trim() !== '' && normalizePricingTier(pricingServiceTier) === '';

  const renderField = (field, total = false) => {
    const key = getFieldKey(field);
    const references = getMultiplierPriceReferences(selectedPricing, field, cacheWriteTTLPricing);
    const groupMultiplier = groupTotalMultiplier * inheritedMultiplier(field.multiplierKey || key);
    const inputId = `${titleId}-${key}`;
    const cacheWrite = pricingProvider === 'codex' && field.label.includes('缓存写入');
    const label = total ? '总倍率' : field.label.replace(/^Long context\s+/, '').replace(/成本倍率|倍率|单价/g, '');
    return (
      <div key={key} className={`flex min-w-0 flex-col rounded-xl border p-3 ${total ? 'border-indigo-100 bg-indigo-50/50' : 'border-slate-200 bg-white'}`}>
        <div className="mb-1.5 flex min-h-5 flex-wrap items-center gap-x-2 gap-y-1">
          <label htmlFor={inputId} className="text-xs font-medium text-slate-700">{label}</label>
          {cacheWrite && <span className="text-[10px] text-indigo-600">{getCacheWritePolicyLabel(cacheWriteTTLPricing)}</span>}
        </div>
        <div className="relative">
          <input
            id={inputId}
            type="text"
            inputMode="decimal"
            value={draft[key] ?? ''}
            onChange={(event) => {
              setDraft((current) => ({ ...current, [key]: event.target.value }));
              if (error) setError('');
            }}
            className="h-9 w-full min-w-0 rounded-lg border border-slate-200 bg-white py-1.5 pl-3 pr-14 font-mono text-sm text-slate-900 outline-none focus:border-indigo-500 focus:ring-2 focus:ring-indigo-500/15 disabled:bg-slate-50 disabled:text-slate-400"
            placeholder={placeholder}
            disabled={disabled}
          />
          <span className="pointer-events-none absolute inset-y-0 right-3 flex items-center text-[11px] text-slate-400">{showPricingReference ? '×' : '$ /1M'}</span>
        </div>
        {!total && (showPricingReference ? (
          <div className="mt-2 space-y-1 text-[11px] text-slate-500">
            {references.map(({ label, price: basePrice }) => <div key={label} className="flex min-w-0 flex-wrap items-center gap-x-1.5 gap-y-0.5 [overflow-wrap:anywhere]">
              {references.length > 1 && label && <span className="mr-auto text-[10px] text-slate-400">{label}</span>}
              {basePrice > 0 ? <><span>{formatPrice(basePrice)}</span><ArrowRight aria-hidden="true" size={11} className="shrink-0 text-slate-300" /><strong className="font-medium text-slate-800">{formatPrice(basePrice * resolveMultiplier(key) * totalMultiplier * groupMultiplier) || '$0'}</strong><span className="text-slate-400">/1M</span></> : <span className="text-slate-400">未配置单价</span>}
            </div>)}
          </div>
        ) : inheritedMultipliers && <p className="mt-2 text-[11px] text-slate-500">实际 <strong className="font-medium text-slate-700">{formatPrice(resolveMultiplier(key) * groupMultiplier) || '$0'}</strong> /1M</p>)}
      </div>
    );
  };

  const handleSave = () => {
    for (const field of fields) {
      const value = parseMultiplierExpression(draft[getFieldKey(field)], Number.NaN);
      if (!Number.isFinite(value) || value < 0) {
        setError(`${field.label}必须是非负数字或分数，例如 1.25 或 40/30`);
        return;
      }
    }
    onSave?.(draft);
  };

  return renderModalPortal(
    <div className="fixed inset-0 z-[120] flex items-center justify-center bg-slate-900/40 p-4 backdrop-blur-sm animate-fade-in">
      <div role="dialog" aria-modal="true" aria-labelledby={titleId} className="flex max-h-[calc(100dvh-2rem)] w-full max-w-2xl flex-col overflow-hidden rounded-2xl bg-white shadow-2xl">
        <div className="flex items-center justify-between border-b border-slate-100 px-5 py-4">
          <div className="min-w-0">
            <h3 id={titleId} className="text-base font-semibold text-slate-900">{title}</h3>
            {description && <p className="mt-0.5 text-xs text-slate-500">{description}</p>}
          </div>
          <button type="button" onClick={onCancel} className="shrink-0 rounded-lg p-2 text-slate-400 hover:bg-slate-100 hover:text-slate-700" aria-label="关闭" disabled={disabled}>
            <XCircle size={18} />
          </button>
        </div>

        <div className="min-h-0 overflow-y-auto bg-slate-50/60 px-5 py-4">
          {(showPricingReference || totalField) && <div className={`mb-3 grid gap-3 ${showPricingReference && totalField ? 'min-[480px]:grid-cols-[minmax(0,1fr)_12rem]' : ''}`}>
            {showPricingReference && <div className="min-w-0 rounded-xl border border-slate-200 bg-white p-3">
              <div className="mb-1.5 flex min-h-5 items-center gap-1.5 text-xs font-medium text-slate-700"><CircleDollarSign size={14} className="text-slate-400" />定价对照</div>
              {visiblePricings.length > 0 ? <CustomSelect
                options={visiblePricings.map((item) => ({
                  value: pricingKey(item),
                  label: <span className="min-w-0 [overflow-wrap:anywhere]">{item.displayName || item.modelName}{!standardOnly && ` · ${pricingTierLabel(item.serviceTier)}`}</span>,
                  description: item.displayName && item.displayName.toLowerCase() !== item.modelName.toLowerCase() ? item.modelName : undefined,
                  badge: item.isDefault ? '默认' : undefined
                }))}
                value={pricingModel}
                onChange={setPricingModel}
                ariaLabel="定价对照模型"
                size="md"
                fullWidth
                disabled={disabled}
                wrapOptions
                className="[&>button]:h-9 [&>button]:gap-2 [&>button]:bg-slate-50/70 [&>button]:text-left [&>button>span]:min-w-0"
              /> : <p className="text-xs text-slate-400">尚未配置{pricingServiceTier ? `${pricingTierLabel(pricingServiceTier)}档` : ''}模型定价</p>}
            </div>}
            {totalField && renderField(totalField, true)}
          </div>}
          <div className="grid grid-cols-1 gap-3 min-[480px]:grid-cols-2">{componentFields.map((field) => renderField(field))}</div>
        </div>

        {error && <p className="px-5 pb-3 text-sm text-red-600">{error}</p>}

        <div className="flex justify-end gap-3 border-t border-slate-100 px-5 py-4">
          <Button type="button" variant="ghost" onClick={onCancel} disabled={disabled}>取消</Button>
          <Button type="button" variant="primary" onClick={handleSave} disabled={disabled}>保存设置</Button>
        </div>
      </div>
    </div>
  );
};

export default MultiplierSettingsModal;
