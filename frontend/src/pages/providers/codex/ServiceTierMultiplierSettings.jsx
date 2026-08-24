/* eslint-disable react-refresh/only-export-components */
import { useState } from 'react';
import { X } from 'lucide-react';
import { Button, SegmentedTabs } from '@components/ui';
import { renderModalPortal } from '@components/ui/modalPortal.js';
import { formatMultiplierInputValue, parseMultiplierExpression } from '@utils/multiplier.js';

export const CODEX_SERVICE_TIERS = [
  { key: 'standard', label: 'Standard', description: '标准处理' },
  { key: 'batch', label: 'Batch', description: '批处理' },
  { key: 'flex', label: 'Flex', description: '弹性处理' },
  { key: 'fast', label: 'Fast mode', description: '快速处理' }
];

export const SERVICE_TIER_MULTIPLIER_FIELDS = [
  { key: 'costMultiplier', label: '总成本倍率' },
  { key: 'inputCostMultiplier', label: '输入成本倍率' },
  { key: 'outputCostMultiplier', label: '输出成本倍率' },
  { key: 'cacheCreationCostMultiplier', label: '缓存写入倍率' },
  { key: 'cacheReadCostMultiplier', label: '缓存读取倍率' }
];

const normalizeTier = (value) => {
  const tier = String(value || '').trim().toLowerCase();
  if (tier === 'priority') return 'fast';
  if (tier === 'default' || tier === 'standard_only') return 'standard';
  return tier;
};

export const buildInitialServiceTierMultipliers = (records = []) => {
  const configured = new Map(
    (Array.isArray(records) ? records : []).map((record) => [
      normalizeTier(record?.serviceTier ?? record?.service_tier),
      record
    ])
  );
  return CODEX_SERVICE_TIERS.map(({ key }) => {
    const record = configured.get(key) || {};
    return {
      serviceTier: key,
      ...Object.fromEntries(SERVICE_TIER_MULTIPLIER_FIELDS.map(({ key: field }) => [
        field,
        formatMultiplierInputValue(record[field] ?? record[field.replace(/[A-Z]/g, (letter) => `_${letter.toLowerCase()}`)] ?? 1)
      ]))
    };
  });
};

export const parseServiceTierMultipliers = (records) => {
  const payload = [];
  for (const tier of CODEX_SERVICE_TIERS) {
    const record = (Array.isArray(records) ? records : []).find((item) => normalizeTier(item?.serviceTier ?? item?.service_tier) === tier.key) || {};
    const parsed = { serviceTier: tier.key };
    for (const field of SERVICE_TIER_MULTIPLIER_FIELDS) {
      const value = parseMultiplierExpression(record[field.key], Number.NaN);
      if (!Number.isFinite(value) || value <= 0) {
        return { error: `${tier.label} ${field.label}必须是大于 0 的数字或分数，例如 1.25 或 40/30` };
      }
      parsed[field.key] = value;
    }
    payload.push(parsed);
  }
  return { payload };
};

const ServiceTierMultiplierSettings = ({ values, disabled = false, onCancel, onSave }) => {
  const [activeTier, setActiveTier] = useState('standard');
  const [draft, setDraft] = useState(() => buildInitialServiceTierMultipliers(values));
  const [error, setError] = useState('');
  const activeRecord = draft.find((record) => record.serviceTier === activeTier) || draft[0];
  const activeMeta = CODEX_SERVICE_TIERS.find((tier) => tier.key === activeTier) || CODEX_SERVICE_TIERS[0];

  const updateField = (field, value) => {
    setDraft((current) => current.map((record) => (
      record.serviceTier === activeTier ? { ...record, [field]: value } : record
    )));
    if (error) setError('');
  };

  const handleSave = () => {
    const result = parseServiceTierMultipliers(draft);
    if (result.error) {
      setError(result.error);
      return;
    }
    onSave?.(result.payload);
  };

  return renderModalPortal(
    <div className="fixed inset-0 z-[120] flex items-center justify-center bg-slate-900/40 p-4 backdrop-blur-sm animate-fade-in" onClick={(event) => event.target === event.currentTarget && onCancel?.()}>
      <div role="dialog" aria-modal="true" aria-labelledby="service-tier-settings-title" className="flex max-h-[calc(100vh-2rem)] w-full max-w-2xl flex-col overflow-hidden rounded-2xl bg-white shadow-2xl">
        <div className="flex items-center justify-between border-b border-slate-100 px-5 py-4">
          <div className="min-w-0">
            <h3 id="service-tier-settings-title" className="text-base font-semibold text-slate-900">Codex 服务档位倍率</h3>
            <p className="mt-0.5 text-xs text-slate-500">分别配置 Standard、Batch、Flex 与 Fast mode。</p>
          </div>
          <button type="button" onClick={onCancel} className="rounded-lg p-2 text-slate-400 hover:bg-slate-100 hover:text-slate-700" aria-label="关闭" disabled={disabled}>
            <X size={18} />
          </button>
        </div>

        <div className="overflow-y-auto px-5 py-4">
          <SegmentedTabs
            tabs={CODEX_SERVICE_TIERS}
            activeTab={activeTier}
            onChange={setActiveTier}
            disabled={disabled}
            className="grid-cols-4"
            buttonPaddingClass="px-1"
          />
          <div className="mt-4 flex items-center justify-between border-b border-slate-100 pb-3">
            <span className="text-sm font-semibold text-slate-800">{activeMeta.label}</span>
            <span className="text-xs text-slate-500">{activeMeta.description}</span>
          </div>
          <div className="mt-4 grid grid-cols-1 gap-3 sm:grid-cols-2">
            {SERVICE_TIER_MULTIPLIER_FIELDS.map((field) => (
              <div key={field.key}>
                <label className="mb-1 block text-xs font-medium text-slate-600">{field.label}</label>
                <div className="relative">
                  <span className="absolute left-3 top-1/2 -translate-y-1/2 text-sm text-slate-400">x</span>
                  <input
                    type="text"
                    inputMode="decimal"
                    value={activeRecord?.[field.key] ?? ''}
                    onChange={(event) => updateField(field.key, event.target.value)}
                    className="w-full rounded-lg border border-slate-200 py-2 pl-8 pr-3 text-sm focus:border-indigo-500 focus:outline-none focus:ring-2 focus:ring-indigo-500/20"
                    placeholder="1.0"
                    disabled={disabled}
                  />
                </div>
              </div>
            ))}
          </div>
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

export default ServiceTierMultiplierSettings;
