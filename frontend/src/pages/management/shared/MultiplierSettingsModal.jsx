import { useEffect, useState } from 'react';
import { XCircle } from 'lucide-react';
import { Button, CustomSelect } from '@components/ui';
import { renderModalPortal } from '@components/ui/modalPortal.js';
import { getModelPricingsByProvider } from '@utils/wailsApi.js';
import { parseMultiplierExpression } from '@utils/multiplier.js';

const getFieldKey = (field) => field.key || field.name;

// 倍率是相对量，脱离基础单价无从判断填得对不对。这里展示所选模型的基础单价，
// 以及按 tracker.go CalculateCost 的口径算出的实际单价（单价 × 分项倍率 × 总倍率）。
const formatPrice = (value) => {
  const num = Number(value);
  if (!Number.isFinite(num) || num <= 0) return null;
  return `$${Number(num.toFixed(4))}`;
};

const normalizePricingTier = (value) => {
  const tier = String(value ?? '').trim().toLowerCase();
  if (!tier || tier === 'default' || tier === 'standard' || tier === 'standard_only') return '';
  if (tier === 'priority') return 'fast';
  return tier;
};

const pricingTierLabel = (value) => ({
  '': 'Standard',
  batch: 'Batch',
  flex: 'Flex',
  fast: 'Fast mode'
}[normalizePricingTier(value)] || String(value || 'Standard'));

const pricingKey = (item) => JSON.stringify([
  item?.modelName || '',
  normalizePricingTier(item?.serviceTier)
]);

const filterPricingsByTier = (records, serviceTier) => {
  // 空档位是组合规则的“全部档位”，省略该 prop 则保持普通倍率弹窗的全量参考行为。
  if (serviceTier === undefined || serviceTier === null || String(serviceTier).trim() === '') return records;
  const normalizedTier = normalizePricingTier(serviceTier);
  return records.filter((item) => normalizePricingTier(item?.serviceTier) === normalizedTier);
};

const MultiplierSettingsModal = ({
  title,
  description,
  fields,
  values,
  pricingProvider = 'codex',
  pricingServiceTier,
  showPricingReference = true,
  placeholder = '1.0',
  disabled = false,
  onCancel,
  onSave
}) => {
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
        const defaultPricing = visiblePricings.find((item) => item.isDefault) || visiblePricings[0];
        setPricingModel(defaultPricing ? pricingKey(defaultPricing) : '');
      })
      // 定价只是填写时的对照参考，取不到不该挡住倍率配置
      .catch(() => {});
    return () => { cancelled = true; };
  }, [pricingProvider, pricingServiceTier, showPricingReference]);

  const visiblePricings = filterPricingsByTier(pricings, pricingServiceTier);
  const selectedPricing = visiblePricings.find((item) => pricingKey(item) === pricingModel) || null;

  // 非法输入按 1.0 预览，校验交给保存时的 handleSave，避免边打字边报错
  const resolveMultiplier = (key) => {
    const parsed = parseMultiplierExpression(draft[key], Number.NaN);
    return Number.isFinite(parsed) && parsed >= 0 ? parsed : 1;
  };

  // 没有 priceKey 的那项是总倍率，作用于全部分项
  const totalField = fields.find((field) => !field.priceKey);
  const totalMultiplier = totalField ? resolveMultiplier(getFieldKey(totalField)) : 1;

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
      <div role="dialog" aria-modal="true" aria-labelledby="multiplier-settings-title" className="w-full max-w-xl overflow-hidden rounded-2xl bg-white shadow-2xl">
        <div className="flex items-center justify-between border-b border-slate-100 px-5 py-4">
          <div>
            <h3 id="multiplier-settings-title" className="text-base font-semibold text-slate-900">{title}</h3>
            {description && <p className="mt-0.5 text-xs text-slate-500">{description}</p>}
          </div>
          <button type="button" onClick={onCancel} className="rounded-lg p-2 text-slate-400 hover:bg-slate-100 hover:text-slate-700" aria-label="关闭" disabled={disabled}>
            <XCircle size={18} />
          </button>
        </div>

        {showPricingReference && (
          <div className="flex items-center gap-3 border-b border-slate-100 bg-slate-50/60 px-5 py-3">
            <span className="shrink-0 text-xs font-medium text-slate-600">定价对照</span>
            {visiblePricings.length > 0 ? (
              <div className="min-w-0 flex-1">
                <CustomSelect
                  options={visiblePricings.map((item) => ({
                    value: pricingKey(item),
                    label: `${item.displayName || item.modelName} · ${pricingTierLabel(item.serviceTier)}`
                  }))}
                  value={pricingModel}
                  onChange={setPricingModel}
                  ariaLabel="定价对照模型"
                  size="sm"
                  fullWidth
                />
              </div>
            ) : (
              <span className="text-xs text-slate-400">
                尚未配置{pricingServiceTier ? `${pricingTierLabel(pricingServiceTier)}档` : ''}模型定价
              </span>
            )}
          </div>
        )}

        <div className="grid max-h-[60vh] grid-cols-1 gap-3 overflow-y-auto px-5 py-4 sm:grid-cols-2">
          {fields.map((field) => {
            const key = getFieldKey(field);
            const basePrice = field.priceKey ? selectedPricing?.[field.priceKey] : null;
            const formattedBase = formatPrice(basePrice);
            return (
              <div key={key}>
                <label className="mb-1 block text-xs text-slate-500">{field.label}</label>
                <input
                  type="text"
                  inputMode="decimal"
                  value={draft[key] ?? ''}
                  onChange={(event) => {
                    setDraft((current) => ({ ...current, [key]: event.target.value }));
                    if (error) setError('');
                  }}
                  className="w-full rounded-lg border border-slate-200 px-3 py-2 text-sm focus:border-indigo-500 focus:outline-none focus:ring-2 focus:ring-indigo-500/20"
                  placeholder={placeholder}
                  disabled={disabled}
                />
                {showPricingReference && (!field.priceKey ? (
                  <p className="mt-1 text-[11px] text-slate-400">作用于全部分项</p>
                ) : formattedBase ? (
                  <p className="mt-1 text-[11px] text-slate-400">
                    {formattedBase} → <span className="font-medium text-slate-600">
                      {formatPrice(basePrice * resolveMultiplier(key) * totalMultiplier) || '$0'}
                    </span> /1M
                  </p>
                ) : (
                  <p className="mt-1 text-[11px] text-slate-300">该模型未配置此项定价</p>
                ))}
              </div>
            );
          })}
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
