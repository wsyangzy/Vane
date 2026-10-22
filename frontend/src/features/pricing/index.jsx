// ============================================
// Pricing 页面 - 模型定价管理
// 2025-12-06 (新增)
// ============================================

import { useState, useEffect, useCallback } from 'react';
import {
  DollarSign,
  Plus,
  Pencil,
  Trash2,
  AlertTriangle,
  Database,
  RefreshCw,
  Star,
  Calculator,
  TrendingUp,
  TrendingDown,
  Sparkles
} from 'lucide-react';
import {
  Button,
  LoadingSpinner,
  ErrorMessage,
  Tooltip,
  useToast
} from '@/shared/ui/index.js';
import {
  getModelPricingStorageStatus,
  getModelPricings,
  createModelPricing,
  updateModelPricing,
  deleteModelPricing,
  deleteModelPricingTier,
  setDefaultModelPricing
} from '@/services/wails/index.js';
import {
  PRICING_SERVICE_TIERS,
  serviceTierLabel,
  emptyTierValues,
  tierValuesFromRecord,
  groupPricingsByModel,
  buildTierPayloads
} from './pricingGroups.js';

// ============================================
// 定价表单弹窗
// ============================================

const LONG_CONTEXT_PRICE_FIELDS = [
  ['longContextInputPrice', '输入价格'],
  ['longContextOutputPrice', '输出价格'],
  ['longContextCacheReadPrice', '缓存读取价格'],
  ['longContextCacheWritePrice', '缓存写入价格']
];

// 表单按「基础信息 + 每个档位一页」分 tab：档位是定价的维度，不是模型的属性，
// 一个模型可以同时配标准档 + batch + flex + fast 四份价格
const BASIC_TAB = { key: 'basic', label: '基础信息' };
const tierTabKey = (tier) => `tier:${tier}`;

// Tailwind JIT 不解析拼接出来的类名，tab 列数只能写死映射
const TAB_GRID_COLS = {
  2: 'grid-cols-2',
  3: 'grid-cols-3',
  4: 'grid-cols-4',
  5: 'grid-cols-5'
};

// 单个档位的价格字段。标准档与各档位子行共用同一套字段，抽出来避免每档一份重复 JSX。
const TierPriceFields = ({ provider, values, errors, onChange, onSuggestCache }) => {
  const longContextEnabled = values.longContextThreshold > 0;

  return (
    <div className="space-y-4">
      <div className="bg-slate-50 rounded-xl p-4 space-y-4">
        <div className="flex items-center justify-between">
          <h4 className="text-sm font-semibold text-slate-700">价格设置</h4>
          {provider === 'claude' && (
            <button
              type="button"
              onClick={onSuggestCache}
              className="text-xs text-indigo-600 hover:text-indigo-700 flex items-center gap-1"
            >
              <Sparkles size={12} />
              自动计算缓存价格
            </button>
          )}
        </div>

        <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
          <div>
            <label className="block text-xs font-medium text-slate-600 mb-1">输入价格</label>
            <div className="relative">
              <span className="absolute left-3 top-1/2 -translate-y-1/2 text-slate-400 text-sm">$</span>
              <input
                type="number"
                step="0.001"
                min="0"
                value={values.inputPrice}
                onChange={(e) => onChange('inputPrice', parseFloat(e.target.value) || 0)}
                className={`w-full pl-7 pr-3 py-2 border rounded-lg text-sm focus:outline-none focus:ring-2 focus:ring-indigo-500 ${
                  errors.inputPrice ? 'border-rose-300' : 'border-slate-200'
                }`}
              />
            </div>
            {errors.inputPrice && <p className="text-xs text-rose-500 mt-1">{errors.inputPrice}</p>}
          </div>

          <div>
            <label className="block text-xs font-medium text-slate-600 mb-1">输出价格</label>
            <div className="relative">
              <span className="absolute left-3 top-1/2 -translate-y-1/2 text-slate-400 text-sm">$</span>
              <input
                type="number"
                step="0.001"
                min="0"
                value={values.outputPrice}
                onChange={(e) => onChange('outputPrice', parseFloat(e.target.value) || 0)}
                className={`w-full pl-7 pr-3 py-2 border rounded-lg text-sm focus:outline-none focus:ring-2 focus:ring-indigo-500 ${
                  errors.outputPrice ? 'border-rose-300' : 'border-slate-200'
                }`}
              />
            </div>
            {errors.outputPrice && <p className="text-xs text-rose-500 mt-1">{errors.outputPrice}</p>}
          </div>

          {/* 缓存价格 - 根据 provider 显示不同字段 */}
          {provider === 'codex' ? (
            <>
              <div>
                <label className="block text-xs font-medium text-slate-600 mb-1">缓存读取价格</label>
                <div className="relative">
                  <span className="absolute left-3 top-1/2 -translate-y-1/2 text-slate-400 text-sm">$</span>
                  <input
                    type="number"
                    step="0.001"
                    min="0"
                    value={values.cacheReadPrice}
                    onChange={(e) => onChange('cacheReadPrice', parseFloat(e.target.value) || 0)}
                    className="w-full pl-7 pr-3 py-2 border border-slate-200 rounded-lg text-sm focus:outline-none focus:ring-2 focus:ring-indigo-500"
                  />
                </div>
              </div>
              <div>
                <label className="block text-xs font-medium text-slate-600 mb-1">缓存写入价格</label>
                <div className="relative">
                  <span className="absolute left-3 top-1/2 -translate-y-1/2 text-slate-400 text-sm">$</span>
                  <input
                    type="number"
                    step="0.001"
                    min="0"
                    value={values.cacheCreationPrice5m}
                    onChange={(e) => onChange('cacheCreationPrice5m', parseFloat(e.target.value) || 0)}
                    className="w-full pl-7 pr-3 py-2 border border-slate-200 rounded-lg text-sm focus:outline-none focus:ring-2 focus:ring-indigo-500"
                  />
                </div>
              </div>
            </>
          ) : (
            <>
              <div>
                <label className="block text-xs font-medium text-slate-600 mb-1">缓存创建 (5m)</label>
                <div className="relative">
                  <span className="absolute left-3 top-1/2 -translate-y-1/2 text-slate-400 text-sm">$</span>
                  <input
                    type="number"
                    step="0.001"
                    min="0"
                    value={values.cacheCreationPrice5m}
                    onChange={(e) => onChange('cacheCreationPrice5m', parseFloat(e.target.value) || 0)}
                    className="w-full pl-7 pr-3 py-2 border border-slate-200 rounded-lg text-sm focus:outline-none focus:ring-2 focus:ring-indigo-500"
                  />
                </div>
                <span className="text-[10px] text-slate-400">input x 1.25</span>
              </div>

              <div>
                <label className="block text-xs font-medium text-slate-600 mb-1">缓存创建 (1h)</label>
                <div className="relative">
                  <span className="absolute left-3 top-1/2 -translate-y-1/2 text-slate-400 text-sm">$</span>
                  <input
                    type="number"
                    step="0.001"
                    min="0"
                    value={values.cacheCreationPrice1h}
                    onChange={(e) => onChange('cacheCreationPrice1h', parseFloat(e.target.value) || 0)}
                    className="w-full pl-7 pr-3 py-2 border border-slate-200 rounded-lg text-sm focus:outline-none focus:ring-2 focus:ring-indigo-500"
                  />
                </div>
                <span className="text-[10px] text-slate-400">input x 2.0</span>
              </div>

              <div className="col-span-2">
                <label className="block text-xs font-medium text-slate-600 mb-1">缓存读取价格</label>
                <div className="relative">
                  <span className="absolute left-3 top-1/2 -translate-y-1/2 text-slate-400 text-sm">$</span>
                  <input
                    type="number"
                    step="0.001"
                    min="0"
                    value={values.cacheReadPrice}
                    onChange={(e) => onChange('cacheReadPrice', parseFloat(e.target.value) || 0)}
                    className="w-full pl-7 pr-3 py-2 border border-slate-200 rounded-lg text-sm focus:outline-none focus:ring-2 focus:ring-indigo-500"
                  />
                </div>
                <span className="text-[10px] text-slate-400">input x 0.1</span>
              </div>
            </>
          )}
        </div>
      </div>

      {/* Long context 定价仅 Codex 适用，且每个档位各自持有一套四价 */}
      {provider === 'codex' && (
        <div className="border border-slate-200 rounded-xl p-4 space-y-4">
          <div className="flex items-center justify-between gap-3">
            <h4 className="text-sm font-semibold text-slate-700">Long context 定价</h4>
            <span className={`inline-flex items-center rounded-full border px-2 py-0.5 text-[10px] font-medium ${
              longContextEnabled
                ? 'border-emerald-200 bg-emerald-50 text-emerald-700'
                : 'border-slate-200 bg-slate-50 text-slate-500'
            }`}>
              {longContextEnabled ? '已启用' : '未启用'}
            </span>
          </div>

          <div>
            <label className="block text-xs font-medium text-slate-600 mb-1">
              启用阈值 (tokens，超过此值后启用)
            </label>
            <input
              type="number"
              step="1"
              min="0"
              value={values.longContextThreshold}
              onChange={(e) => onChange('longContextThreshold', Number(e.target.value) || 0)}
              className={`w-full px-3 py-2 border rounded-lg text-sm focus:outline-none focus:ring-2 focus:ring-indigo-500 ${
                errors.longContextThreshold ? 'border-rose-300' : 'border-slate-200'
              }`}
            />
            {errors.longContextThreshold && (
              <p className="text-xs text-rose-500 mt-1">{errors.longContextThreshold}</p>
            )}
          </div>

          <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
            {LONG_CONTEXT_PRICE_FIELDS.map(([field, label]) => (
              <div key={field}>
                <label className="block text-xs font-medium text-slate-600 mb-1">{label}</label>
                <div className="relative">
                  <span className="absolute left-3 top-1/2 -translate-y-1/2 text-slate-400 text-sm">$</span>
                  <input
                    type="number"
                    step="0.001"
                    min="0"
                    value={values[field]}
                    disabled={!longContextEnabled}
                    onChange={(e) => onChange(field, parseFloat(e.target.value) || 0)}
                    className={`w-full pl-7 pr-3 py-2 border rounded-lg text-sm focus:outline-none focus:ring-2 focus:ring-indigo-500 disabled:bg-slate-100 disabled:text-slate-400 ${
                      errors[field] ? 'border-rose-300' : 'border-slate-200'
                    }`}
                  />
                </div>
                {errors[field] && <p className="text-xs text-rose-500 mt-1">{errors[field]}</p>}
              </div>
            ))}
          </div>
        </div>
      )}
    </div>
  );
};

// 从已有定价行还原表单的档位映射。缺失的档位留空壳，用户勾选「启用」后再填。
const buildTierFormState = (group) => {
  const tiers = {};
  for (const { value: tier } of PRICING_SERVICE_TIERS) {
    tiers[tier] = emptyTierValues();
  }
  if (group?.standard) {
    tiers[''] = tierValuesFromRecord(group.standard);
  } else {
    // 新建模型时标准档给一份 Claude Sonnet 量级的初值，省得四个价从 0 敲起
    tiers[''] = {
      ...emptyTierValues(),
      enabled: true,
      inputPrice: 3.0,
      outputPrice: 15.0,
      cacheCreationPrice5m: 3.75,
      cacheCreationPrice1h: 6.0,
      cacheReadPrice: 0.3
    };
  }
  for (const record of group?.tiers || []) {
    tiers[record.serviceTier] = tierValuesFromRecord(record);
  }
  return tiers;
};

const PricingForm = ({ pricing, onSave, onCancel, loading }) => {
  const isEdit = !!pricing;
  const [formData, setFormData] = useState({
    modelName: pricing?.modelName || '',
    displayName: pricing?.displayName || '',
    description: pricing?.description || '',
    provider: pricing?.provider || 'claude',
    isDefault: pricing?.isDefault || false,
    tiers: buildTierFormState(pricing)
  });
  // errors: { modelName, submit, tiers: { '': { inputPrice: '...' } } }
  const [errors, setErrors] = useState({});
  const [activeTab, setActiveTab] = useState('basic');

  const handleChange = (field, value) => {
    setFormData(prev => ({ ...prev, [field]: value }));
    if (errors[field]) {
      setErrors(prev => ({ ...prev, [field]: null }));
    }
  };

  const handleTierChange = (tier, field, value) => {
    setFormData(prev => ({
      ...prev,
      tiers: { ...prev.tiers, [tier]: { ...prev.tiers[tier], [field]: value } }
    }));
    if (errors.tiers?.[tier]?.[field]) {
      setErrors(prev => ({
        ...prev,
        tiers: { ...prev.tiers, [tier]: { ...prev.tiers[tier], [field]: null } }
      }));
    }
  };

  // 首次启用某档位时，从标准档带入一份初值：档位价通常是标准价的折扣或溢价，
  // 全 0 起步既费事又容易漏填成 $0 计费
  const handleTierToggle = (tier) => {
    setFormData(prev => {
      const current = prev.tiers[tier];
      const turningOn = !current.enabled;
      const seeded = turningOn && current.id === 0 && current.inputPrice === 0 && current.outputPrice === 0
        ? { ...prev.tiers[''], id: 0 }
        : current;
      return {
        ...prev,
        tiers: { ...prev.tiers, [tier]: { ...seeded, enabled: turningOn } }
      };
    });
  };

  const validateTier = (values, provider) => {
    const tierErrors = {};
    if (values.inputPrice < 0) tierErrors.inputPrice = '输入价格不能为负数';
    if (values.outputPrice < 0) tierErrors.outputPrice = '输出价格不能为负数';
    // Long context 定价仅 Codex 适用，Claude 下这些字段不可见，跳过校验
    // 否则历史脏数据会把 activeTab 切到一个不存在的 tab，表单卡死无法提交
    if (provider === 'codex') {
      if (!Number.isInteger(values.longContextThreshold) || values.longContextThreshold < 0) {
        tierErrors.longContextThreshold = 'Long context 阈值必须是非负整数';
      }
      LONG_CONTEXT_PRICE_FIELDS.forEach(([field, label]) => {
        if (values[field] < 0) {
          tierErrors[field] = `Long context ${label}不能为负数`;
        }
      });
    }
    return tierErrors;
  };

  const validate = () => {
    const newErrors = {};
    const tierErrors = {};

    if (!formData.modelName.trim()) {
      newErrors.modelName = '模型名称不能为空';
    }

    for (const { value: tier } of PRICING_SERVICE_TIERS) {
      if (tier !== '' && formData.provider !== 'codex') continue;
      const values = formData.tiers[tier];
      if (tier !== '' && !values.enabled) continue;
      const found = validateTier(values, formData.provider);
      if (Object.keys(found).length > 0) tierErrors[tier] = found;
    }

    if (Object.keys(tierErrors).length > 0) newErrors.tiers = tierErrors;
    setErrors(newErrors);

    // 把 tab 切到第一个出错的页面，否则报错落在看不见的 tab 上，用户只会看到「保存无反应」
    const firstBadTier = PRICING_SERVICE_TIERS.find(({ value }) => tierErrors[value]);
    if (newErrors.modelName) {
      setActiveTab('basic');
    } else if (firstBadTier) {
      setActiveTab(tierTabKey(firstBadTier.value));
    }
    return !newErrors.modelName && Object.keys(tierErrors).length === 0;
  };

  const handleSubmit = async (e) => {
    e.preventDefault();
    if (!validate()) return;

    try {
      await onSave(formData);
    } catch (err) {
      setErrors({ submit: err.message });
    }
  };

  // 自动计算缓存价格建议 (按 Anthropic 官方定价规则)
  // 5分钟缓存: input × 1.25, 1小时缓存: input × 2.0, 读取: input × 0.1
  const suggestCachePrices = (tier) => {
    setFormData(prev => {
      const values = prev.tiers[tier];
      const input = parseFloat(values.inputPrice) || 0;
      return {
        ...prev,
        tiers: {
          ...prev.tiers,
          [tier]: {
            ...values,
            cacheCreationPrice5m: parseFloat((input * 1.25).toFixed(3)),
            cacheCreationPrice1h: parseFloat((input * 2.0).toFixed(3)),
            cacheReadPrice: parseFloat((input * 0.1).toFixed(3))
          }
        }
      };
    });
  };

  // 档位子行仅 Codex 适用：Claude 只有基础信息 + 标准档两页
  const showTierTabs = formData.provider === 'codex';
  const visibleFormTabs = [
    BASIC_TAB,
    ...(showTierTabs ? PRICING_SERVICE_TIERS : PRICING_SERVICE_TIERS.filter((t) => t.value === ''))
      .map(({ value, label }) => ({ key: tierTabKey(value), label, tier: value }))
  ];

  // 切到 Claude 时若正停留在档位 tab，回落到标准档，避免停在已隐藏的 tab 上
  const handleProviderChange = (provider) => {
    setFormData((prev) => ({ ...prev, provider }));
    if (provider !== 'codex' && activeTab !== 'basic' && activeTab !== tierTabKey('')) {
      setActiveTab(tierTabKey(''));
    }
  };

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center overflow-hidden bg-black/50 p-4 animate-fade-in">
      <div className="w-full max-w-2xl rounded-2xl bg-white shadow-xl">
        <form onSubmit={handleSubmit} className="flex max-h-[calc(100vh-2rem)] flex-col">
          {/* 头部 */}
          <div className="px-6 py-4 border-b border-slate-100">
            <div className="flex items-center justify-between">
              <div className="flex items-center gap-3">
                <div className="p-2 bg-indigo-100 rounded-lg">
                  <DollarSign className="text-indigo-600" size={20} />
                </div>
                <div>
                  <h3 className="text-lg font-semibold text-slate-900">
                    {isEdit ? '编辑模型定价' : '添加模型定价'}
                  </h3>
                  <p className="text-sm text-slate-500">USD per 1M tokens</p>
                </div>
              </div>
              {/* 提供商切换 */}
              {isEdit ? (
                <span className={`inline-flex items-center rounded-full border px-3 py-1.5 text-xs font-medium ${
                  formData.provider === 'codex'
                    ? 'border-emerald-200 bg-emerald-50 text-emerald-700'
                    : 'border-indigo-200 bg-indigo-50 text-indigo-700'
                }`}>
                  {formData.provider === 'codex' ? 'Codex' : 'Claude'}
                </span>
              ) : (
                <div className="flex items-center gap-1 bg-slate-100 rounded-lg p-0.5">
                  <button
                    type="button"
                    onClick={() => handleProviderChange('claude')}
                    className={`px-2.5 py-1 text-xs font-medium rounded-md transition-colors ${
                      formData.provider === 'claude'
                        ? 'bg-white text-indigo-700 shadow-sm'
                        : 'text-slate-500 hover:text-slate-700'
                    }`}
                  >
                    Claude
                  </button>
                  <button
                    type="button"
                    onClick={() => handleProviderChange('codex')}
                    className={`px-2.5 py-1 text-xs font-medium rounded-md transition-colors ${
                      formData.provider === 'codex'
                        ? 'bg-white text-emerald-700 shadow-sm'
                        : 'text-slate-500 hover:text-slate-700'
                    }`}
                  >
                    Codex
                  </button>
                </div>
              )}
            </div>
          </div>

          <div className="border-b border-slate-100 bg-slate-50/70 px-6 py-3">
            <div className={`grid ${TAB_GRID_COLS[visibleFormTabs.length] || 'grid-cols-5'} rounded-xl bg-slate-100 p-1`}>
              {visibleFormTabs.map((tab) => {
                // 未启用的档位在 tab 上就能看出来，不用逐页点开确认
                const configured = tab.tier === '' || formData.tiers[tab.tier]?.enabled;
                return (
                  <button
                    key={tab.key}
                    type="button"
                    onClick={() => setActiveTab(tab.key)}
                    className={`flex items-center justify-center gap-1.5 rounded-lg px-3 py-2 text-sm font-medium transition-colors ${
                      activeTab === tab.key
                        ? 'bg-white text-indigo-700 shadow-sm ring-1 ring-slate-200'
                        : 'text-slate-500 hover:text-slate-700'
                    }`}
                  >
                    {tab.label}
                    {tab.tier !== undefined && (
                      <span className={`h-1.5 w-1.5 rounded-full ${configured ? 'bg-emerald-500' : 'bg-slate-300'}`} />
                    )}
                  </button>
                );
              })}
            </div>
          </div>

          {/* 表单内容 */}
          <div className="min-h-0 overflow-y-auto px-6 py-4 space-y-4">
            {activeTab === 'basic' && (
              <>
            {/* 模型名称 */}
            <div>
              <label className="block text-sm font-medium text-slate-700 mb-1.5">
                模型名称 <span className="text-rose-500">*</span>
              </label>
              <input
                type="text"
                value={formData.modelName}
                onChange={(e) => handleChange('modelName', e.target.value)}
                placeholder="例如: claude-sonnet-4-20250514"
                disabled={isEdit}
                className={`w-full px-3 py-2 border rounded-lg text-sm focus:outline-none focus:ring-2 focus:ring-indigo-500 ${
                  errors.modelName ? 'border-rose-300' : 'border-slate-200'
                } ${isEdit ? 'bg-slate-50 text-slate-500' : ''}`}
              />
              {errors.modelName && (
                <p className="text-xs text-rose-500 mt-1">{errors.modelName}</p>
              )}
            </div>

            {/* 档位不是模型的属性，而是定价的维度：一个模型可同时挂标准档 + batch/flex/fast
                四份价格，因此档位在各自的 tab 里配置，不在这里单选 */}
            {formData.provider === 'codex' && (
              <p className="rounded-lg bg-slate-50 px-3 py-2 text-xs text-slate-500">
                标准档为该模型的兜底定价；Batch / Flex / Fast 各有独立的一套价格，
                在对应 tab 里勾选启用后填写，只在上游返回该档位时命中。
              </p>
            )}

            {/* 显示名称 */}
            <div>
              <label className="block text-sm font-medium text-slate-700 mb-1.5">
                显示名称
              </label>
              <input
                type="text"
                value={formData.displayName}
                onChange={(e) => handleChange('displayName', e.target.value)}
                placeholder="例如: Claude Sonnet 4"
                className="w-full px-3 py-2 border border-slate-200 rounded-lg text-sm focus:outline-none focus:ring-2 focus:ring-indigo-500"
              />
            </div>

            {/* 描述 */}
            <div>
              <label className="block text-sm font-medium text-slate-700 mb-1.5">
                描述
              </label>
              <input
                type="text"
                value={formData.description}
                onChange={(e) => handleChange('description', e.target.value)}
                placeholder="模型描述"
                className="w-full px-3 py-2 border border-slate-200 rounded-lg text-sm focus:outline-none focus:ring-2 focus:ring-indigo-500"
              />
            </div>

              </>
            )}

            {/* 每个档位一页：标准档与各档位子行各自持有一套完整价格 */}
            {visibleFormTabs.map((tab) => {
              if (tab.tier === undefined || activeTab !== tab.key) return null;
              const values = formData.tiers[tab.tier];
              const tierEnabled = tab.tier === '' || values.enabled;
              return (
                <div key={tab.key} className="space-y-4">
                  {tab.tier === '' ? (
                    <p className="rounded-lg bg-slate-50 px-3 py-2 text-xs text-slate-500">
                      标准档是该模型的兜底定价，始终生效；未单独配置的档位一律回落到这里计费。
                    </p>
                  ) : (
                    <div
                      onClick={() => handleTierToggle(tab.tier)}
                      className={`flex cursor-pointer items-center justify-between rounded-xl border p-3 transition-all duration-200 ${
                        values.enabled
                          ? 'border-violet-200 bg-violet-50 shadow-sm'
                          : 'border-slate-200 bg-slate-50 hover:border-slate-300'
                      }`}
                    >
                      <div>
                        <span className={`text-sm font-medium ${values.enabled ? 'text-violet-700' : 'text-slate-600'}`}>
                          启用 {tab.label} 档位定价
                        </span>
                        <p className="text-[11px] text-slate-400">
                          {values.enabled
                            ? '上游返回该档位时按下方价格计费'
                            : '未启用时该档位回落到标准档价格'}
                        </p>
                      </div>
                      <div className={`relative h-5 w-9 rounded-full transition-colors duration-200 ${
                        values.enabled ? 'bg-violet-500' : 'bg-slate-300'
                      }`}>
                        <div className={`absolute top-0.5 h-4 w-4 rounded-full bg-white shadow-sm transition-transform duration-200 ${
                          values.enabled ? 'translate-x-4' : 'translate-x-0.5'
                        }`} />
                      </div>
                    </div>
                  )}

                  {tierEnabled && (
                    <TierPriceFields
                      provider={formData.provider}
                      values={values}
                      errors={errors.tiers?.[tab.tier] || {}}
                      onChange={(field, value) => handleTierChange(tab.tier, field, value)}
                      onSuggestCache={() => suggestCachePrices(tab.tier)}
                    />
                  )}
                </div>
              );
            })}

            {/* 设为默认 */}
            {activeTab === 'basic' && (
            <div
              onClick={() => handleChange('isDefault', !formData.isDefault)}
              className={`flex items-center justify-between p-3 rounded-xl border cursor-pointer transition-all duration-200 ${
                formData.isDefault
                  ? 'bg-amber-50 border-amber-200 shadow-sm'
                  : 'bg-slate-50 border-slate-200 hover:border-slate-300'
              }`}
            >
              <div className="flex items-center gap-2.5">
                <Star size={16} className={formData.isDefault ? 'text-amber-500 fill-amber-500' : 'text-slate-400'} />
                <div>
                  <span className={`text-sm font-medium ${formData.isDefault ? 'text-amber-700' : 'text-slate-600'}`}>
                    默认定价
                  </span>
                  <p className="text-[11px] text-slate-400">同类型未知模型将使用此定价</p>
                </div>
              </div>
              <div className={`relative w-9 h-5 rounded-full transition-colors duration-200 ${
                formData.isDefault ? 'bg-amber-500' : 'bg-slate-300'
              }`}>
                <div className={`absolute top-0.5 w-4 h-4 rounded-full bg-white shadow-sm transition-transform duration-200 ${
                  formData.isDefault ? 'translate-x-4' : 'translate-x-0.5'
                }`} />
              </div>
            </div>
            )}

            {/* 错误提示 */}
            {errors.submit && (
              <div className="p-3 bg-rose-50 border border-rose-100 rounded-lg text-sm text-rose-600">
                {errors.submit}
              </div>
            )}
          </div>

          {/* 底部按钮 */}
          <div className="px-6 py-4 border-t border-slate-100 flex justify-end gap-3">
            <Button variant="ghost" type="button" onClick={onCancel} disabled={loading}>
              取消
            </Button>
            <Button type="submit" loading={loading}>
              {isEdit ? '保存修改' : '创建定价'}
            </Button>
          </div>
        </form>
      </div>
    </div>
  );
};

// ============================================
// 删除确认对话框
// ============================================

const DeleteConfirmDialog = ({ pricing, onConfirm, onCancel, loading }) => (
  <div className="fixed inset-0 bg-black/50 flex items-start justify-center z-50 animate-fade-in pt-[20vh]">
    <div className="bg-white rounded-2xl shadow-xl w-full max-w-md p-6">
      <div className="flex items-center gap-3 mb-4">
        <div className="p-3 bg-rose-100 rounded-full">
          <AlertTriangle className="text-rose-600" size={24} />
        </div>
        <div>
          <h3 className="text-lg font-semibold text-slate-900">确认删除</h3>
          <p className="text-sm text-slate-500">此操作不可撤销</p>
        </div>
      </div>

      <p className="text-slate-700 mb-6">
        确定要删除模型定价 <span className="font-semibold">&ldquo;{pricing?.displayName || pricing?.modelName}&rdquo;</span> 吗？
        该模型在此提供商下的<span className="font-semibold">全部档位定价</span>将一并删除，删除后将无法恢复。
      </p>

      <div className="flex justify-end gap-3">
        <Button variant="ghost" onClick={onCancel} disabled={loading}>
          取消
        </Button>
        <Button
          variant="danger"
          icon={Trash2}
          onClick={onConfirm}
          loading={loading}
        >
          确认删除
        </Button>
      </div>
    </div>
  </div>
);

// ============================================
// 定价卡片组件
// ============================================

const PricingCard = ({ group, onEdit, onDelete, onSetDefault }) => {
  const isDefault = group.isDefault;
  // 卡片主体展示标准档价格；标准档缺失（历史脏数据）时退到第一条档位子行，
  // 否则这张卡会整片空白，用户只能靠改库才能收拾这些行
  const pricing = group.standard || group.tiers[0] || {};

  // 计算价格等级颜色
  const getPriceLevel = (price) => {
    if (price >= 10) return 'text-rose-600';
    if (price >= 3) return 'text-amber-600';
    return 'text-emerald-600';
  };

  return (
    <div className={`
      bg-white rounded-xl border shadow-sm hover:shadow-md transition-all flex flex-col
      ${isDefault
        ? group.provider === 'codex'
          ? 'border-blue-300 ring-2 ring-blue-100'
          : 'border-orange-300 ring-2 ring-orange-100'
        : 'border-slate-200/60'}
    `}>
      {/* 头部 */}
      <div className="border-b border-slate-100 px-4 py-3">
        <div className="flex items-start justify-between">
          <div className="flex-1 min-w-0">
            <div className="flex min-w-0 items-center gap-1.5">
              <h3 className="truncate text-sm font-semibold text-slate-900" title={group.displayName || group.modelName}>
                {group.displayName || group.modelName}
              </h3>
              {isDefault && (
                <span className={`inline-flex shrink-0 items-center whitespace-nowrap px-1.5 py-0.5 rounded-full text-[10px] font-medium border ${
                  group.provider === 'codex'
                    ? 'bg-blue-100 text-blue-700 border-blue-200'
                    : 'bg-orange-100 text-orange-700 border-orange-200'
                }`}>
                  <Star size={10} className="mr-1" />
                  默认
                </span>
              )}
              <span className={`inline-flex shrink-0 items-center whitespace-nowrap px-1.5 py-0.5 rounded-full text-[10px] font-medium border ${
                group.provider === 'codex'
                  ? 'bg-emerald-50 text-emerald-700 border-emerald-200'
                  : 'bg-indigo-50 text-indigo-700 border-indigo-200'
              }`}>
                {group.provider === 'codex' ? 'Codex' : 'Claude'}
              </span>
            </div>
            <p className="text-xs text-slate-400 font-mono mt-1 truncate" title={group.modelName}>
              {group.modelName}
            </p>
          </div>
          <div className="ml-2 flex shrink-0 items-center gap-1">
            {!isDefault && (
              <Tooltip content="设为默认">
                <button
                  onClick={() => onSetDefault(group.modelName)}
                  className="p-1.5 text-slate-400 hover:bg-amber-50 hover:text-amber-600 rounded-md transition-colors"
                  aria-label="设为默认"
                >
                  <Star size={14} />
                </button>
              </Tooltip>
            )}
            <Tooltip content="编辑">
              <button
                onClick={() => onEdit(group)}
                className="p-1.5 text-slate-400 hover:bg-slate-100 hover:text-indigo-600 rounded-md transition-colors"
                aria-label="编辑"
              >
                <Pencil size={14} />
              </button>
            </Tooltip>
            {!isDefault && (
              <Tooltip content="删除">
                <button
                  onClick={() => onDelete(group)}
                  className="p-1.5 text-slate-400 hover:bg-rose-50 hover:text-rose-600 rounded-md transition-colors"
                  aria-label="删除"
                >
                  <Trash2 size={14} />
                </button>
              </Tooltip>
            )}
          </div>
        </div>
        {group.description && (
          <p className="text-xs text-slate-500 mt-2 line-clamp-2">{group.description}</p>
        )}
        {/* 已配置的档位一眼可见：一个模型可同时挂多档价格，不展开就看不出来 */}
        {group.tiers.length > 0 && (
          <div className="mt-2 flex flex-wrap items-center gap-1.5">
            <span className="text-[10px] text-slate-400">附加档位</span>
            {group.tiers.map((tier) => (
              <span
                key={tier.serviceTier}
                className="inline-flex items-center rounded-full border border-violet-200 bg-violet-50 px-2 py-0.5 text-[10px] font-medium text-violet-700"
              >
                {serviceTierLabel(tier.serviceTier)} ${tier.inputPrice}/${tier.outputPrice}
              </span>
            ))}
          </div>
        )}
      </div>

      {/* 价格信息 */}
      <div className="p-4">
        <div className="grid grid-cols-2 gap-3">
          {/* 输入价格 */}
          <div className="bg-slate-50 rounded-lg p-3">
            <div className="flex items-center gap-1.5 text-xs text-slate-500 mb-1">
              <TrendingDown size={12} />
              输入
            </div>
            <div className={`text-lg font-bold ${getPriceLevel(pricing.inputPrice)}`}>
              ${pricing.inputPrice}
            </div>
          </div>

          {/* 输出价格 */}
          <div className="bg-slate-50 rounded-lg p-3">
            <div className="flex items-center gap-1.5 text-xs text-slate-500 mb-1">
              <TrendingUp size={12} />
              输出
            </div>
            <div className={`text-lg font-bold ${getPriceLevel(pricing.outputPrice)}`}>
              ${pricing.outputPrice}
            </div>
          </div>

          {/* 缓存价格 - 根据 provider 显示不同字段 */}
          {pricing.provider === 'codex' ? (
            <>
              <div className="bg-emerald-50/50 rounded-lg p-3">
                <div className="text-xs text-slate-500 mb-1">缓存读取</div>
                <div className="text-sm font-semibold text-emerald-600">
                  ${pricing.cacheReadPrice}
                </div>
              </div>
              <div className="bg-amber-50/50 rounded-lg p-3">
                <div className="text-xs text-slate-500 mb-1">缓存写入</div>
                <div className="text-sm font-semibold text-amber-600">
                  ${pricing.cacheCreationPrice5m}
                </div>
              </div>
            </>
          ) : (
            <>
              <div className="bg-indigo-50/50 rounded-lg p-3">
                <div className="text-xs text-slate-500 mb-1">缓存创建 (5m)</div>
                <div className="text-sm font-semibold text-indigo-600">
                  ${pricing.cacheCreationPrice5m}
                </div>
              </div>
              <div className="bg-violet-50/50 rounded-lg p-3">
                <div className="text-xs text-slate-500 mb-1">缓存创建 (1h)</div>
                <div className="text-sm font-semibold text-violet-600">
                  ${pricing.cacheCreationPrice1h}
                </div>
              </div>
              <div className="col-span-2 bg-emerald-50/50 rounded-lg p-3">
                <div className="text-xs text-slate-500 mb-1">缓存读取</div>
                <div className="text-sm font-semibold text-emerald-600">
                  ${pricing.cacheReadPrice}
                </div>
              </div>
            </>
          )}
        </div>

        {pricing.longContextThreshold > 0 && (
          <div className="mt-4 border-t border-slate-200 pt-3">
            <div className="mb-2 flex items-center justify-between gap-3">
              <span className="text-xs font-semibold text-slate-600">Long context</span>
              <span className="text-[10px] font-medium text-slate-500">
                ≥ {pricing.longContextThreshold.toLocaleString()} tokens
              </span>
            </div>
            <div className="grid grid-cols-2 gap-x-4 gap-y-1.5 text-xs">
              <span className="text-slate-500">输入</span>
              <span className="text-right font-semibold text-slate-700">${pricing.longContextInputPrice}</span>
              <span className="text-slate-500">输出</span>
              <span className="text-right font-semibold text-slate-700">${pricing.longContextOutputPrice}</span>
              <span className="text-slate-500">缓存读取</span>
              <span className="text-right font-semibold text-emerald-700">${pricing.longContextCacheReadPrice}</span>
              <span className="text-slate-500">缓存写入</span>
              <span className="text-right font-semibold text-amber-700">${pricing.longContextCacheWritePrice}</span>
            </div>
          </div>
        )}
      </div>

      {/* 底部信息 */}
      <div className="px-4 py-2 border-t border-slate-50 text-xs text-slate-400 mt-auto">
        更新于 {group.updatedAt || '-'}
      </div>
    </div>
  );
};

// ============================================
// Pricing 页面
// ============================================

const PricingPage = () => {
  const toast = useToast();

  // 数据状态
  const [storageStatus, setStorageStatus] = useState(null);
  const [pricings, setPricings] = useState([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);

  // 表单状态
  const [showForm, setShowForm] = useState(false);
  const [editingPricing, setEditingPricing] = useState(null);
  const [formLoading, setFormLoading] = useState(false);

  // 删除确认状态
  const [deleteTarget, setDeleteTarget] = useState(null);
  const [deleteLoading, setDeleteLoading] = useState(false);

  // 加载数据
  const loadData = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const [status, records] = await Promise.all([
        getModelPricingStorageStatus(),
        getModelPricings()
      ]);
      setStorageStatus(status);
      setPricings(records);
    } catch (err) {
      console.error('加载模型定价失败:', err);
      setError(err.message);
    } finally {
      setLoading(false);
    }
  }, []);

  // 初始化加载
  useEffect(() => {
    loadData();
  }, [loadData]);

  // 新建定价
  const handleCreate = () => {
    setEditingPricing(null);
    setShowForm(true);
  };

  // 编辑定价
  const handleEdit = (pricing) => {
    setEditingPricing(pricing);
    setShowForm(true);
  };

  // 删除定价
  const handleDelete = (pricing) => {
    setDeleteTarget(pricing);
  };

  // 设为默认
  const handleSetDefault = async (modelName) => {
    try {
      await setDefaultModelPricing(modelName);
      await loadData();
    } catch (err) {
      console.error('设置默认定价失败:', err);
      toast.error(err.message, '设置默认定价失败');
    }
  };

  // 保存定价。一次保存要落多行：启用的档位按 id 走新建或更新，取消启用的档位删掉
  const handleSave = async (formData) => {
    setFormLoading(true);
    try {
      const { saves, deletes } = buildTierPayloads(formData);
      for (const payload of saves) {
        if (payload.id > 0) {
          await updateModelPricing(payload.modelName, payload);
        } else {
          await createModelPricing(payload);
        }
      }
      // 删除放在保存之后：任一保存失败时被停用的档位仍在，
      // 不会留下「新价格没存上、旧档位已经删掉」的半截状态。
      // 删除用改名前的 provider/modelName——被停用的档位行不在 saves 里，没跟着改名
      const origin = editingPricing || formData;
      for (const tier of deletes) {
        await deleteModelPricingTier(origin.provider, origin.modelName, tier);
      }
      setShowForm(false);
      setEditingPricing(null);
      await loadData();
    } catch (err) {
      console.error('保存失败:', err);
      throw err;
    } finally {
      setFormLoading(false);
    }
  };

  // 确认删除。卡片删除的是整个模型，连同其全部档位子行一起删——
  // 残留的子行仍会被 provider::model::tier 命中，表现为「模型已删除但请求还在按档位计费」
  const handleConfirmDelete = async () => {
    if (!deleteTarget) return;

    setDeleteLoading(true);
    try {
      await deleteModelPricing(deleteTarget.provider, deleteTarget.modelName);
      setDeleteTarget(null);
      await loadData();
    } catch (err) {
      console.error('删除失败:', err);
      toast.error(err.message, '删除失败');
    } finally {
      setDeleteLoading(false);
    }
  };

  // 存储未启用
  if (!loading && storageStatus && !storageStatus.enabled) {
    return (
      <div className="animate-fade-in">
        <div className="flex flex-col items-center justify-center py-20">
          <div className="p-4 bg-slate-100 rounded-full mb-4">
            <Database size={40} className="text-slate-400" />
          </div>
          <h3 className="text-lg font-semibold text-slate-700 mb-2">模型定价服务未启用</h3>
          <p className="text-sm text-slate-500 max-w-md text-center mb-4">
            请在配置文件中启用 <code className="bg-slate-100 px-1.5 py-0.5 rounded text-xs">usage_tracking.enabled: true</code>
          </p>
        </div>
      </div>
    );
  }

  // 错误状态
  if (error) {
    return (
      <ErrorMessage
        title="数据加载失败"
        message={error}
        onRetry={loadData}
      />
    );
  }

  // 加载状态
  if (loading && pricings.length === 0) {
    return <LoadingSpinner text="加载模型定价..." />;
  }

  // 档位不是模型的属性而是定价的维度：同一模型的多档定价聚合成一张卡片
  const pricingGroups = groupPricingsByModel(pricings);

  // 统计数据
  const claudeDefault = pricings.find(p => p.isDefault && p.provider === 'claude');
  const codexDefault = pricings.find(p => p.isDefault && p.provider === 'codex');
  // 均价只按模型的标准档算：把折扣档位一起平均会得到一个没有意义的数
  const primaryRows = pricingGroups.map((group) => group.standard || group.tiers[0]).filter(Boolean);
  const avgInputPrice = primaryRows.length > 0
    ? (primaryRows.reduce((sum, p) => sum + p.inputPrice, 0) / primaryRows.length).toFixed(2)
    : 0;
  const avgOutputPrice = primaryRows.length > 0
    ? (primaryRows.reduce((sum, p) => sum + p.outputPrice, 0) / primaryRows.length).toFixed(2)
    : 0;
  const pricingSummaryCards = [
    {
      title: '模型数',
      value: pricingGroups.length,
      icon: Database,
      statusColor: 'bg-slate-100 text-slate-700'
    },
    {
      title: 'Claude 默认',
      value: claudeDefault?.displayName || claudeDefault?.modelName || '未设置',
      tooltip: claudeDefault?.displayName || claudeDefault?.modelName || '未设置',
      icon: Star,
      statusColor: 'bg-orange-100 text-orange-700',
      wrapValue: true,
      valueClassName: 'text-left'
    },
    {
      title: 'Codex 默认',
      value: codexDefault?.displayName || codexDefault?.modelName || '未设置',
      tooltip: codexDefault?.displayName || codexDefault?.modelName || '未设置',
      icon: Star,
      statusColor: 'bg-blue-100 text-blue-700',
      wrapValue: true,
      valueClassName: 'text-left'
    },
    {
      title: '平均输入价格',
      value: `$${avgInputPrice}`,
      icon: TrendingDown,
      statusColor: 'bg-slate-100 text-slate-700'
    },
    {
      title: '平均输出价格',
      value: `$${avgOutputPrice}`,
      icon: TrendingUp,
      statusColor: 'bg-slate-100 text-slate-700'
    }
  ];

  return (
    <div className="animate-fade-in">
      {/* 页面标题 */}
      <div className="mb-3 flex min-h-9 flex-wrap items-center justify-between gap-3">
        <div className="flex min-w-0 items-center gap-2.5">
          <div className="shrink-0 rounded-lg bg-slate-900 p-1.5 text-white shadow-sm">
            <DollarSign className="h-5 w-5" />
          </div>
          <div className="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1">
            <h1 className="text-xl font-bold text-slate-900">基础定价</h1>
            <p className="text-xs text-slate-500">
              USD / 1M Tokens
            </p>
          </div>
        </div>
        <div className="flex items-center gap-3">
          {/* 刷新按钮 */}
          <Button
            variant="ghost"
            size="sm"
            icon={RefreshCw}
            onClick={loadData}
            loading={loading}
          >
            刷新
          </Button>

          {/* 添加按钮 */}
          <Button icon={Plus} onClick={handleCreate}>
            添加定价
          </Button>
        </div>
      </div>

      {/* 统计卡片 */}
      <div className="mb-3 grid auto-rows-fr grid-cols-2 overflow-hidden rounded-xl border border-slate-200 bg-white min-[900px]:grid-cols-[minmax(6.5rem,0.65fr)_1.35fr_1.35fr_1fr_1fr] min-[900px]:divide-x min-[900px]:divide-slate-100">
        {pricingSummaryCards.map((card) => (
          <Tooltip
            key={card.title}
            content={card.tooltip || card.value}
            className="block h-full min-w-0"
          >
            <div className="flex h-full min-w-0 items-center gap-2.5 p-3">
              <div className={`shrink-0 rounded-lg p-1.5 ${card.statusColor}`}>
                <card.icon size={16} />
              </div>
              <div className="min-w-0">
                <h3 className="text-xs font-medium leading-5 text-slate-500">
                  {card.title}
                </h3>
                <div className={`font-semibold text-slate-900 ${card.wrapValue ? 'truncate text-sm leading-6' : 'break-all text-lg leading-6 tabular-nums'} ${card.valueClassName || ''}`}>
                  {card.value}
                </div>
              </div>
            </div>
          </Tooltip>
        ))}
      </div>

      {/* 定价卡片网格 */}
      {pricingGroups.length === 0 ? (
        <div className="bg-white rounded-2xl border border-slate-200/60 shadow-sm p-12 text-center">
          <div className="flex flex-col items-center gap-3">
            <Calculator size={40} className="text-slate-300" />
            <p className="text-slate-500">暂无定价配置</p>
            <Button icon={Plus} onClick={handleCreate}>
              添加第一个定价
            </Button>
          </div>
        </div>
      ) : (
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
          {pricingGroups.map((group) => (
            <PricingCard
              key={group.key}
              group={group}
              onEdit={handleEdit}
              onDelete={handleDelete}
              onSetDefault={handleSetDefault}
            />
          ))}
        </div>
      )}

      {/* 表单弹窗 */}
      {showForm && (
        <PricingForm
          pricing={editingPricing}
          onSave={handleSave}
          onCancel={() => {
            setShowForm(false);
            setEditingPricing(null);
          }}
          loading={formLoading}
        />
      )}

      {/* 删除确认弹窗 */}
      {deleteTarget && (
        <DeleteConfirmDialog
          pricing={deleteTarget}
          onConfirm={handleConfirmDelete}
          onCancel={() => setDeleteTarget(null)}
          loading={deleteLoading}
        />
      )}
    </div>
  );
};

export default PricingPage;
