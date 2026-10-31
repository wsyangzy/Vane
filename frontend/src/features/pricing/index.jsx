// ============================================
// Pricing 页面 - 模型定价管理
// 2025-12-06 (新增)
// ============================================

import { useState, useEffect, useCallback, useId, useRef } from 'react';
import {
  DollarSign,
  X,
  Check,
  ChevronDown,
  ExternalLink,
  BadgeDollarSign,
  Plus,
  Pencil,
  Trash2,
  AlertTriangle,
  Database,
  RefreshCw,
  Star,
  Calculator,
  Sparkles
} from 'lucide-react';
import {
  Button,
  LoadingSpinner,
  ErrorMessage,
  Tooltip,
  useToast
} from '@/shared/ui/index.js';
import { renderModalPortal } from '@/shared/ui/modalPortal.js';
import { ClaudeIcon } from '@/features/requests/components/ModelTag.jsx';
import { CodexLogo } from '@/features/providers/codex/components/CodexLogo.jsx';
import {
  getModelPricingStorageStatus,
  getModelPricings,
  createModelPricing,
  updateModelPricing,
  deleteModelPricing,
  deleteModelPricingTier,
  setDefaultModelPricing,
  openExternalURL
} from '@/services/wails/index.js';
import {
  PRICING_SERVICE_TIERS,
  serviceTierLabel,
  emptyTierValues,
  tierValuesFromRecord,
  groupPricingsByModel,
  groupPricingSpeeds,
  buildTierPayloads
} from './pricingGroups.js';
import './pricing.css';

// ============================================
// 定价表单弹窗
// ============================================

// Keep the form and cards in the same order as each provider's pricing table.
// The existing storage fields remain provider-specific, including Codex cache writes.
const PRICE_FIELDS = {
  claude: [
    { field: 'inputPrice', label: '基础输入', term: 'Base input tokens', description: '未命中缓存、也未写入缓存的输入 Token。' },
    { field: 'cacheCreationPrice5m', label: '缓存写入 · 5 分钟', term: '5m cache writes', description: '将输入内容写入缓存，缓存有效期为 5 分钟。' },
    { field: 'cacheCreationPrice1h', label: '缓存写入 · 1 小时', term: '1h cache writes', description: '将输入内容写入缓存，缓存有效期为 1 小时。' },
    { field: 'cacheReadPrice', label: '缓存命中与刷新', term: 'Cache hits and refreshes', description: '读取已有缓存，并刷新缓存有效期。' },
    { field: 'outputPrice', label: '输出', term: 'Output tokens', description: '模型生成内容所消耗的输出 Token。' },
  ],
  codex: [
    { field: 'inputPrice', label: '输入（未缓存）', term: 'Input', description: '未命中缓存、也未单独按缓存写入计价的输入 Token。' },
    { field: 'outputPrice', label: '输出（含推理）', term: 'Output', description: '模型生成的内容及计入输出的推理 Token，推理用量不重复叠加。' },
    { field: 'cacheReadPrice', label: '缓存输入', term: 'Cached input', description: '复用已有提示词缓存的输入 Token，即缓存读取。' },
    { field: 'cacheWritePrice30m', label: '缓存写入 · 30 分钟', term: '30m write', description: '默认缓存写入单价；只有上游报告写入用量时计费，不重复计算普通输入。' },
    { field: 'cacheWritePrice12h', label: '缓存写入 · 12 小时', term: '12h write', description: '可选；分组启用 TTL 分档计费且上游返回分档用量时生效，否则仍按 30m。' },
  ],
};
const priceFields = (provider) => PRICE_FIELDS[provider] || PRICE_FIELDS.claude;
const LONG_CONTEXT_FIELDS = {
  inputPrice: 'longContextInputPrice',
  outputPrice: 'longContextOutputPrice',
  cacheReadPrice: 'longContextCacheReadPrice',
  cacheWritePrice30m: 'longContextCacheWritePrice',
  cacheWritePrice12h: 'longContextCacheWritePrice12h',
};
const LONG_CONTEXT_PRICE_FIELDS = PRICE_FIELDS.codex.map(({ field, label }) => [LONG_CONTEXT_FIELDS[field], label]);
const officialPricingURL = (provider) => provider === 'codex'
  ? 'https://developers.openai.com/api/docs/pricing'
  : 'https://platform.claude.com/docs/en/about-claude/pricing';

// 档位是定价的维度，一个模型可同时配置多份价格。
const BASIC_TAB = { key: 'basic', label: '基础信息' };
const tierTabKey = (tier) => `tier:${tier}`;
const TAB_GRID_COLS = { 2: 'grid-cols-2', 3: 'grid-cols-3', 4: 'grid-cols-4', 5: 'grid-cols-5', 6: 'grid-cols-6' };

const usePricingDialog = (onCancel, loading) => {
  const dialogRef = useRef(null);
  useEffect(() => {
    const previousFocus = document.activeElement;
    const initialControl = dialogRef.current?.querySelector('[data-autofocus]');
    (initialControl || dialogRef.current)?.focus();
    return () => {
      if (previousFocus?.isConnected) previousFocus.focus();
    };
  }, []);

  const onKeyDown = (event) => {
    if (event.key === 'Escape') {
      event.preventDefault();
      event.stopPropagation();
      if (!loading) onCancel();
    }
    if (event.key !== 'Tab') return;
    const controls = [...dialogRef.current.querySelectorAll('button:not(:disabled), input:not(:disabled), [href], [tabindex="0"]')]
      .filter((element) => element.getClientRects().length);
    const first = controls[0];
    const last = controls.at(-1);
    if (!first) {
      event.preventDefault();
    } else if (event.shiftKey && (document.activeElement === first || document.activeElement === dialogRef.current)) {
      event.preventDefault();
      last.focus();
    } else if (!event.shiftKey && (document.activeElement === last || document.activeElement === dialogRef.current)) {
      event.preventDefault();
      first.focus();
    }
  };

  return { dialogRef, onKeyDown };
};

const PriceInput = ({ definition, field = definition.field, values, error, disabled, onChange }) => {
  const inputId = useId();
  return <Tooltip asChild content={<><span className="block text-[11px] text-slate-300">{definition.term}</span>{definition.description}</>}><div className="pricing-field-card" data-field={definition.field}>
    <label htmlFor={inputId} className="pricing-field-title">{definition.label}</label>
    <span id={`${inputId}-description`} className="sr-only">{definition.term}。{definition.description}</span>
    <div className="pricing-field-value">
      <div className="pricing-currency-input"><span aria-hidden="true">$</span><input id={inputId} type="number" step="any" min="0" value={values[field]} disabled={disabled} aria-invalid={Boolean(error)} aria-describedby={`${inputId}-description${error ? ` ${inputId}-error` : ''}`} onChange={(event) => onChange(field, parseFloat(event.target.value) || 0)} /></div>
      {error && <p id={`${inputId}-error`} className="mt-1 text-xs text-rose-500">{error}</p>}
    </div>
  </div></Tooltip>;
};

const TierPriceFields = ({ provider, values, errors, onChange, onSuggestCache }) => {
  const longContextEnabled = values.longContextThreshold > 0;
  return <div className="space-y-3">
    <section className="pricing-fields-section" data-provider={provider} aria-label="Token 单价">
      <header className="pricing-fields-heading"><h4>Token 单价 <small>USD / 100 万 Token</small></h4><button type="button" className="pricing-text-action" onClick={() => openExternalURL(officialPricingURL(provider))}><ExternalLink size={12} />官方定价</button></header>
      <div className="pricing-fields-grid">{priceFields(provider).map((definition) => <PriceInput key={definition.field} definition={definition} values={values} error={errors[definition.field]} onChange={onChange} />)}</div>
      {provider === 'claude' && <div className="pricing-cache-helper"><Tooltip asChild content="按基础输入价计算：写入 5 分钟 ×1.25、1 小时 ×2、读取 ×0.1。部分模型倍率不同，请按官方或中转站报价调整。"><button type="button" className="pricing-text-action" onClick={onSuggestCache}><Sparkles size={12} />填入缓存参考价</button></Tooltip></div>}
    </section>
    {provider === 'codex' && <section className="pricing-fields-section" data-provider={provider} aria-label="长上下文定价">
      <header className="pricing-fields-heading"><div><h4>长上下文定价 <small>Long context</small></h4><p>仅超过输入阈值的请求使用这组单价</p></div><span className="pricing-context-state" data-enabled={longContextEnabled}>{longContextEnabled ? '已启用' : '未启用'}</span></header>
      <div className="pricing-threshold"><label htmlFor="pricing-context-threshold">输入阈值 <small>Token</small></label><input id="pricing-context-threshold" type="number" step="1" min="0" value={values.longContextThreshold} aria-invalid={Boolean(errors.longContextThreshold)} aria-describedby="pricing-context-threshold-help" onChange={(event) => onChange('longContextThreshold', Number(event.target.value) || 0)} /><p id="pricing-context-threshold-help">输入总量超过该值时启用，包含缓存输入；填 0 表示关闭。单价单位同上。</p>{errors.longContextThreshold && <p role="alert" className="pricing-threshold-error">{errors.longContextThreshold}</p>}</div>
      {longContextEnabled && <div className="pricing-fields-grid">{PRICE_FIELDS.codex.map((definition) => <PriceInput key={definition.field} definition={definition} field={LONG_CONTEXT_FIELDS[definition.field]} values={values} error={errors[LONG_CONTEXT_FIELDS[definition.field]]} onChange={onChange} />)}</div>}
    </section>}
  </div>;
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

const PricingForm = ({ pricing, initialProvider = 'claude', onSave, onCancel, loading }) => {
  const isEdit = !!pricing;
  const titleId = useId();
  const { dialogRef, onKeyDown } = usePricingDialog(onCancel, loading);
  const [formData, setFormData] = useState({
    modelName: pricing?.modelName || '',
    displayName: pricing?.displayName || '',
    description: pricing?.description || '',
    provider: pricing?.provider || initialProvider,
    isDefault: pricing?.isDefault || false,
    tiers: buildTierFormState(pricing)
  });
  // errors: { modelName, submit, tiers: { '': { inputPrice: '...' } } }
  const [errors, setErrors] = useState({});
  const [activeTab, setActiveTab] = useState('basic');
  const [discountTier, setDiscountTier] = useState('flex');

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
        tierErrors.longContextThreshold = '长上下文阈值必须是非负整数';
      }
      LONG_CONTEXT_PRICE_FIELDS.forEach(([field, label]) => {
        if (values[field] < 0) {
          tierErrors[field] = `长上下文${label}不能为负数`;
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
      if (['batch', 'flex'].includes(firstBadTier.value)) setDiscountTier(firstBadTier.value);
      setActiveTab(tierTabKey(firstBadTier.value === 'batch' ? 'flex' : firstBadTier.value));
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

  // 常用缓存倍率仅作填入建议；部分模型使用不同的读取倍率，需按官方报价核对。
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
    ...(showTierTabs ? PRICING_SERVICE_TIERS.filter((t) => t.value !== 'batch') : PRICING_SERVICE_TIERS.filter((t) => t.value === ''))
      .map(({ value, label }) => ({ key: tierTabKey(value), label: value === 'flex' ? 'Flex/Batch' : label, tier: value }))
  ];

  // 切到 Claude 时若正停留在档位 tab，回落到标准档，避免停在已隐藏的 tab 上
  const handleProviderChange = (provider) => {
    setFormData((prev) => ({ ...prev, provider }));
    if (provider !== 'codex' && activeTab !== 'basic' && activeTab !== tierTabKey('')) {
      setActiveTab(tierTabKey(''));
    }
  };

  // 根层 portal 避免 app-content 的 z-index / transform 将弹窗压在主导航下。
  return renderModalPortal(
    <div className="pricing-modal-layer" onMouseDown={(event) => {
      if (event.target === event.currentTarget && !loading) onCancel();
    }}>
      <div ref={dialogRef} onKeyDown={onKeyDown} role="dialog" aria-modal="true" aria-labelledby={titleId} tabIndex={-1} className="pricing-form-dialog">
        <form onSubmit={handleSubmit} className="pricing-form-layout" aria-busy={loading}>
          {/* 头部 */}
          <div className="pricing-form-header">
            <div className="pricing-form-heading">
              <div className="flex min-w-0 items-center gap-2.5">
                <div className="shrink-0 rounded-lg bg-indigo-50 p-2">
                  <DollarSign className="text-indigo-600" size={18} />
                </div>
                <div>
                  <h3 id={titleId} className="text-base font-semibold text-slate-900">
                    {isEdit ? '编辑模型定价' : '添加模型定价'}
                  </h3>
                  <p className="mt-0.5 text-[11px] text-slate-400">美元 / 100 万 Token · USD / MTok</p>
                </div>
              </div>
              <div className="pricing-form-header-actions">
              {/* 提供商切换 */}
              {isEdit ? (
                <span className="pricing-provider-badge" data-provider={formData.provider}>
                  {formData.provider === 'codex' ? <CodexLogo className="h-3.5 w-3.5" /> : <ClaudeIcon className="h-3.5 w-3.5" />}
                  {formData.provider === 'codex' ? 'Codex' : 'Claude'}
                </span>
              ) : (
                <div className="pricing-provider-switch" role="group" aria-label="提供商">
                  <button
                    type="button"
                    data-provider="claude"
                    aria-pressed={formData.provider === 'claude'}
                    onClick={() => handleProviderChange('claude')}
                    disabled={loading}
                  >
                    <ClaudeIcon className="h-3.5 w-3.5" />
                    Claude
                  </button>
                  <button
                    type="button"
                    data-provider="codex"
                    aria-pressed={formData.provider === 'codex'}
                    onClick={() => handleProviderChange('codex')}
                    disabled={loading}
                  >
                    <CodexLogo className="h-3.5 w-3.5" />
                    Codex
                  </button>
                </div>
              )}
              <button type="button" className="pricing-modal-close" aria-label="关闭定价窗口" onClick={onCancel} disabled={loading}><X size={18} /></button>
              </div>
            </div>
          </div>

          <div className="pricing-form-navigation">
            <div className={`pricing-form-tabs grid ${TAB_GRID_COLS[visibleFormTabs.length] || 'grid-cols-5'}`} role="group" aria-label="定价配置">
              {visibleFormTabs.map((tab) => {
                // 未启用的档位在 tab 上就能看出来，不用逐页点开确认
                const configured = tab.tier === '' || formData.tiers[tab.tier]?.enabled || (tab.tier === 'flex' && formData.tiers.batch?.enabled);
                return (
                  <button
                    key={tab.key}
                    type="button"
                    aria-pressed={activeTab === tab.key}
                    onClick={() => setActiveTab(tab.key)}
                    className={`flex min-w-0 items-center justify-center gap-1.5 rounded-md px-1.5 py-1.5 text-xs font-medium transition-colors ${
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
          <div key={activeTab} className="pricing-form-body">
            {activeTab === 'basic' && (
              <div className="pricing-basic-fields">
            {/* 模型名称 */}
            <div>
              <label htmlFor={`${titleId}-model`} className="block text-xs font-medium text-slate-700 mb-1.5">
                模型名称 <span className="text-rose-500">*</span>
              </label>
              <input
                id={`${titleId}-model`}
                data-autofocus={!isEdit || undefined}
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
            {/* 显示名称 */}
            <div>
              <label htmlFor={`${titleId}-display`} className="block text-xs font-medium text-slate-700 mb-1.5">
                显示名称
              </label>
              <input
                id={`${titleId}-display`}
                data-autofocus={isEdit || undefined}
                type="text"
                value={formData.displayName}
                onChange={(e) => handleChange('displayName', e.target.value)}
                placeholder="例如: Claude Sonnet 4"
                className="w-full px-3 py-2 border border-slate-200 rounded-lg text-sm focus:outline-none focus:ring-2 focus:ring-indigo-500"
              />
            </div>

            {/* 描述 */}
            <div className="pricing-basic-wide">
              <label htmlFor={`${titleId}-description`} className="block text-xs font-medium text-slate-700 mb-1.5">
                描述 <span className="font-normal text-slate-400">（选填）</span>
              </label>
              <input
                id={`${titleId}-description`}
                type="text"
                value={formData.description}
                onChange={(e) => handleChange('description', e.target.value)}
                placeholder="模型描述"
                className="w-full px-3 py-2 border border-slate-200 rounded-lg text-sm focus:outline-none focus:ring-2 focus:ring-indigo-500"
              />
            </div>

            {formData.provider === 'codex' && (
              <p className="pricing-basic-wide pricing-form-note">
                Speed 按请求的最终计费档位匹配；Flex/Batch 保留独立价格。缓存写入默认使用 30m，12h 需在凭证分组中启用分档计费。
              </p>
            )}
              </div>
            )}

            {/* 每个档位一页：标准档与各档位子行各自持有一套完整价格 */}
            {visibleFormTabs.map((tab) => {
              if (tab.tier === undefined || activeTab !== tab.key) return null;
              const tier = tab.tier === 'flex' ? discountTier : tab.tier;
              const values = formData.tiers[tier];
              const tierEnabled = tier === '' || values.enabled;
              return (
                <div key={tab.key} className="space-y-3">
                  {tab.tier === 'flex' && <div className="pricing-tier-tabs" role="group" aria-label="Flex 或 Batch 价格">{['flex', 'batch'].map((value) => <button type="button" key={value} aria-pressed={discountTier === value} onClick={() => setDiscountTier(value)}>{serviceTierLabel(value)}</button>)}</div>}
                  {tier === '' ? (
                    <p className="pricing-form-note">
                      标准档是该模型的兜底定价，始终生效；未单独配置的档位一律回落到这里计费。
                    </p>
                  ) : (
                    <button type="button" role="switch" aria-checked={values.enabled}
                      onClick={() => handleTierToggle(tier)}
                      className={`flex w-full items-center justify-between gap-3 rounded-lg border p-2.5 text-left transition-colors ${
                        values.enabled
                          ? 'border-violet-200 bg-violet-50 shadow-sm'
                          : 'border-slate-200 bg-slate-50 hover:border-slate-300'
                      }`}
                    >
                      <span>
                        <span className={`text-sm font-medium ${values.enabled ? 'text-violet-700' : 'text-slate-600'}`}>
                          启用 {serviceTierLabel(tier)} 档位定价
                        </span>
                        <span className="block text-[11px] text-slate-400">
                          {values.enabled
                            ? '最终计费档位匹配时使用下方价格'
                            : '未启用时该档位回落到标准档价格'}
                        </span>
                      </span>
                      <span aria-hidden="true" className={`relative h-5 w-9 shrink-0 rounded-full transition-colors duration-200 ${
                        values.enabled ? 'bg-violet-500' : 'bg-slate-300'
                      }`}>
                        <span className={`absolute top-0.5 h-4 w-4 rounded-full bg-white shadow-sm transition-transform duration-200 ${
                          values.enabled ? 'translate-x-4' : 'translate-x-0.5'
                        }`} />
                      </span>
                    </button>
                  )}

                  {tierEnabled && (
                    <TierPriceFields
                      provider={formData.provider}
                      values={values}
                      errors={errors.tiers?.[tier] || {}}
                      onChange={(field, value) => handleTierChange(tier, field, value)}
                      onSuggestCache={() => suggestCachePrices(tier)}
                    />
                  )}
                </div>
              );
            })}

            {/* 设为默认 */}
            {activeTab === 'basic' && (
            <button type="button" role="switch" aria-checked={formData.isDefault}
              onClick={() => handleChange('isDefault', !formData.isDefault)}
              className={`flex w-full items-center justify-between gap-3 rounded-lg border p-2.5 text-left transition-colors ${
                formData.isDefault
                  ? 'bg-amber-50 border-amber-200 shadow-sm'
                  : 'bg-slate-50 border-slate-200 hover:border-slate-300'
              }`}
            >
              <span className="flex items-center gap-2.5">
                <Star size={16} className={formData.isDefault ? 'text-amber-500 fill-amber-500' : 'text-slate-400'} />
                <span>
                  <span className={`text-sm font-medium ${formData.isDefault ? 'text-amber-700' : 'text-slate-600'}`}>
                    默认定价
                  </span>
                  <span className="block text-[11px] text-slate-400">同类型未知模型将使用此定价</span>
                </span>
              </span>
              <span aria-hidden="true" className={`relative w-9 h-5 shrink-0 rounded-full transition-colors duration-200 ${
                formData.isDefault ? 'bg-amber-500' : 'bg-slate-300'
              }`}>
                <span className={`absolute top-0.5 w-4 h-4 rounded-full bg-white shadow-sm transition-transform duration-200 ${
                  formData.isDefault ? 'translate-x-4' : 'translate-x-0.5'
                }`} />
              </span>
            </button>
            )}

            {/* 错误提示 */}
            {errors.submit && (
              <div className="p-3 bg-rose-50 border border-rose-100 rounded-lg text-sm text-rose-600">
                {errors.submit}
              </div>
            )}
          </div>

          {/* 底部按钮 */}
          <div className="pricing-form-footer">
            <Button variant="ghost" type="button" onClick={onCancel} disabled={loading}>
              取消
            </Button>
            <Button type="submit" icon={Check} loading={loading}>
              {isEdit ? '保存修改' : '创建定价'}
            </Button>
          </div>
        </form>
      </div>
    </div>,
    { root: true }
  );
};

// ============================================
// 删除确认对话框
// ============================================

const DeleteConfirmDialog = ({ pricing, onConfirm, onCancel, loading }) => {
  const titleId = useId();
  const { dialogRef, onKeyDown } = usePricingDialog(onCancel, loading);
  return renderModalPortal(
    <div className="pricing-modal-layer" onMouseDown={(event) => {
      if (event.target === event.currentTarget && !loading) onCancel();
    }}>
      <div ref={dialogRef} onKeyDown={onKeyDown} role="dialog" aria-modal="true" aria-labelledby={titleId} tabIndex={-1} className="pricing-form-dialog pricing-delete-dialog">
        <div className="pricing-form-body">
          <div className="flex items-center gap-3 mb-4">
            <div className="p-3 bg-rose-100 rounded-full">
              <AlertTriangle className="text-rose-600" size={24} />
            </div>
            <div>
              <h3 id={titleId} className="text-lg font-semibold text-slate-900">确认删除</h3>
              <p className="text-sm text-slate-500">此操作不可撤销</p>
            </div>
          </div>
          <p className="text-sm leading-6 text-slate-600 [overflow-wrap:anywhere]">
            确定要删除模型定价 <span className="font-semibold">&ldquo;{pricing?.displayName || pricing?.modelName}&rdquo;</span> 吗？
            该模型在此提供商下的<span className="font-semibold">全部档位定价</span>将一并删除，删除后将无法恢复。
          </p>
        </div>
        <div className="pricing-form-footer">
          <Button variant="ghost" data-autofocus onClick={onCancel} disabled={loading}>
            取消
          </Button>
          <Button variant="danger" icon={Trash2} onClick={onConfirm} loading={loading}>
            确认删除
          </Button>
        </div>
      </div>
    </div>,
    { root: true }
  );
};

// ============================================
// 定价卡片组件
// ============================================

const PricingCard = ({ group, onEdit, onDelete, onSetDefault }) => {
  const [selectedTier, setSelectedTier] = useState('');
  const records = [...(group.standard ? [group.standard] : []), ...group.tiers];
  const pricing = records.find((record) => record.serviceTier === selectedTier) || records[0] || {};
  const speeds = groupPricingSpeeds(records);
  const selectedSpeed = speeds.find((speed) => speed.records.includes(pricing));
  const providerLabel = group.provider === 'codex' ? 'Codex' : 'Claude';
  const ProviderIcon = group.provider === 'codex' ? CodexLogo : ClaudeIcon;
  const fields = priceFields(group.provider);
  return <article className="pricing-model-card" data-provider={group.provider} data-default={group.isDefault} aria-label={`${providerLabel} ${group.displayName || group.modelName} 定价`}>
    <header className="pricing-model-header">
      <div className="pricing-model-topline">
        <div className="pricing-model-identity">
          <Tooltip asChild content={providerLabel}><span className="pricing-provider-icon" role="img" aria-label={providerLabel}><ProviderIcon className="h-4 w-4" /></span></Tooltip>
          <h3>{group.displayName || group.modelName}</h3>
        </div>
        <div className="pricing-model-actions">
        {!group.isDefault && <Tooltip asChild content="设为默认定价"><button type="button" aria-label={`设为默认定价 ${group.modelName}`} onClick={() => onSetDefault(group.modelName)}><Star size={13} /></button></Tooltip>}
        <Tooltip asChild content="编辑定价"><button type="button" aria-label={`编辑定价 ${group.modelName}`} onClick={() => onEdit(group)}><Pencil size={13} /></button></Tooltip>
        {!group.isDefault && <Tooltip asChild content="删除定价"><button type="button" data-danger aria-label={`删除定价 ${group.modelName}`} onClick={() => onDelete(group)}><Trash2 size={13} /></button></Tooltip>}
        </div>
      </div>
      <p className="pricing-model-id">{group.modelName}</p>
      {group.description && <p className="pricing-model-description">{group.description}</p>}
    </header>
    {speeds.length > 1 && <div className="pricing-tier-tabs" role="group" aria-label="Speed">{speeds.map((speed) => <button type="button" key={speed.key} aria-pressed={speed === selectedSpeed} onClick={() => setSelectedTier(speed.records[0].serviceTier)}>{speed.label}</button>)}</div>}
    {selectedSpeed?.key === 'flex_batch' && selectedSpeed.records.length > 1 && <div className="pricing-tier-tabs" role="group" aria-label="Flex 或 Batch 价格">{selectedSpeed.records.map((record) => <button type="button" key={record.serviceTier} aria-pressed={record === pricing} onClick={() => setSelectedTier(record.serviceTier)}>{serviceTierLabel(record.serviceTier)}</button>)}</div>}
    <div className="pricing-rate-heading"><span>{records.length > 1 ? 'Token 单价' : serviceTierLabel(pricing.serviceTier || '')}</span><span>USD / 100 万 Token</span></div>
    <dl className="pricing-rate-list">{fields.map(({ field, label }) => <div key={field} data-field={field}><dt>{label}</dt><dd><span>$</span>{pricing[field] ?? '—'}</dd></div>)}</dl>
    {group.provider === 'codex' && pricing.longContextThreshold > 0 && <details className="pricing-long-context"><summary><span>长上下文单价</span><span>&gt; {pricing.longContextThreshold.toLocaleString()} Token<ChevronDown size={12} /></span></summary><dl className="pricing-rate-list">{fields.map(({ field, label }) => <div key={field} data-field={field}><dt>{label}</dt><dd><span>$</span>{pricing[LONG_CONTEXT_FIELDS[field]] ?? '—'}</dd></div>)}</dl></details>}
    <footer className="pricing-model-footer"><span className="pricing-model-footer-label">{group.isDefault && <span className="pricing-default-badge"><Star size={10} className="fill-current" />默认定价</span>}{records.length > 1 ? <span>{records.length} 个计费档位</span> : !group.isDefault && '标准定价'}</span><span>更新于 {group.updatedAt || '—'}</span></footer>
  </article>;
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
  const [providerFilter, setProviderFilter] = useState('all');

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
  const visiblePricingGroups = providerFilter === 'all'
    ? pricingGroups
    : pricingGroups.filter((group) => group.provider === providerFilter);
  const providerFilters = [
    { value: 'all', label: '全部', icon: Database, count: pricingGroups.length },
    { value: 'claude', label: 'Claude', icon: ClaudeIcon, count: pricingGroups.filter((group) => group.provider === 'claude').length },
    { value: 'codex', label: 'Codex', icon: CodexLogo, count: pricingGroups.filter((group) => group.provider === 'codex').length }
  ];

  return (
    <div className="pricing-workspace animate-fade-in">
      {/* 页面标题 */}
      <div className="pricing-page-toolbar">
        <div className="flex min-w-0 items-center gap-2.5">
          <div className="shrink-0 rounded-lg bg-slate-900 p-1.5 text-white shadow-sm">
            <BadgeDollarSign className="h-5 w-5" />
          </div>
          <div className="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1">
            <h1 className="text-xl font-bold text-slate-900">基础定价</h1>
            <p className="text-xs text-slate-500">
              USD / 1M Tokens
            </p>
          </div>
        </div>
        <div className="flex shrink-0 items-center gap-1.5">
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
          <Button size="sm" icon={Plus} onClick={handleCreate}>
            添加定价
          </Button>
        </div>
      </div>

      <div className="pricing-list-toolbar">
        <div className="pricing-provider-filters" role="group" aria-label="定价分类">
          {providerFilters.map(({ value, label, icon: Icon, count }) => (
            <button key={value} type="button" aria-pressed={providerFilter === value} onClick={() => setProviderFilter(value)}>
              <Icon size={14} aria-hidden="true" />
              <span>{label}</span>
              <span className="pricing-filter-count">{count}</span>
            </button>
          ))}
        </div>
        <span className="pricing-list-count" aria-live="polite">显示 {visiblePricingGroups.length} / {pricingGroups.length} 个模型</span>
      </div>

      {/* 定价卡片网格 */}
      {visiblePricingGroups.length === 0 ? (
        <div className="bg-white rounded-2xl border border-slate-200/60 shadow-sm p-12 text-center">
          <div className="flex flex-col items-center gap-3">
            <Calculator size={40} className="text-slate-300" />
            <p className="text-slate-500">{providerFilter === 'all' ? '暂无定价配置' : `暂无 ${providerFilter === 'codex' ? 'Codex' : 'Claude'} 定价配置`}</p>
            <Button icon={Plus} onClick={handleCreate}>
              添加定价
            </Button>
          </div>
        </div>
      ) : (
        <div className="pricing-model-grid">
          {visiblePricingGroups.map((group) => (
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
          initialProvider={providerFilter === 'codex' ? 'codex' : 'claude'}
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
