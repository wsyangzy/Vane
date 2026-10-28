import { useState } from 'react';
import { ArrowRight, Eye, EyeOff, KeyRound, Plus, Trash2, XCircle } from 'lucide-react';
import { Button, CustomSelect, Input, SegmentedTabs, Tooltip } from '@/shared/ui/index.js';
import { renderModalPortal } from '@/shared/ui/modalPortal.js';
import SubscriptionSettings from '@/features/providers/shared/subscriptions/SubscriptionSettings.jsx';
import { validateSubscription } from '@/features/providers/shared/subscriptions/subscriptionSettings.js';
import EndpointCopyNameEditorStep, { EndpointCopySourcePicker } from '@/features/providers/shared/endpoints/EndpointCopyNameEditorStep.jsx';
import ModelMultiplierEditor from '@/features/providers/shared/billing/ModelMultiplierEditor.jsx';
import MultiplierSettingsModal from '@/features/providers/shared/billing/MultiplierSettingsModal.jsx';
import BillingRuleEditor, { createDefaultBillingRuleRow, parseBillingRuleRows } from './BillingRuleEditor.jsx';
import { buildModelMappingPayload } from '@/features/providers/shared/routing/modelMapping.js';
import { MULTIPLIER_EXPRESSION_HELP, parseModelMultiplierRows } from '@/features/providers/shared/billing/modelMultiplierRows.js';
import { isWailsEnvironment } from '@/services/wails/index.js';
import { buildInitialCredentialGroupForm, CODEX_CREDENTIAL_GROUP_FORM_TABS, CODEX_CREDENTIAL_GROUP_CREATE_FORM_TABS, CODEX_BILLING_MODES, BILLING_OUTPUT_TOKEN_SOURCE_OPTIONS, STANDARD_ENDPOINT_MULTIPLIER_FIELDS, LONG_CONTEXT_ENDPOINT_MULTIPLIER_FIELDS, parseEndpointMultiplierPayload } from '../model.js';

// ============================================================================
// 凭证分组编辑弹窗
// ============================================================================
const CredentialGroupFormModal = ({ mode, channel, group, continueToEndpoint = false, loading, serverError, endpointCopyOptions = [], onCancel, onSave }) => {
  const [formData, setFormData] = useState(() => buildInitialCredentialGroupForm(group, channel));
  const [standardMultiplierSettingsOpen, setStandardMultiplierSettingsOpen] = useState(false);
  const [longContextSettingsOpen, setLongContextSettingsOpen] = useState(false);  const [activeTab, setActiveTab] = useState('basic');
  const [fieldError, setFieldError] = useState('');
  const [showApiKey, setShowApiKey] = useState(false);
  const isWails = isWailsEnvironment();
  const modalTopPadding = isWails ? 48 : 16;
  const modalBottomPadding = 16;
  const modalMaxHeight = `calc(100vh - ${modalTopPadding + modalBottomPadding}px)`;
  const isEdit = mode === 'edit';
  const [formStep, setFormStep] = useState('details');
  const [copyStepEndpoints, setCopyStepEndpoints] = useState([]);
  const [copyEndpointsEnabled, setCopyEndpointsEnabled] = useState(false);
  const copyChoices = [
    { value: '', label: '不复制', name: '不复制', endpointCount: 0, endpoints: [] },
    ...endpointCopyOptions
  ];
  const selectedCopyOption = copyChoices.find((option) => option.value === (formData.copyEndpointsFromGroupId || '')) || copyChoices[0];
  const canCopyEndpoints = !isEdit && formData.authMode !== 'client_passthrough' && copyChoices.length > 1;
  const credentialGroupTabs = canCopyEndpoints && copyEndpointsEnabled
    ? CODEX_CREDENTIAL_GROUP_CREATE_FORM_TABS
    : CODEX_CREDENTIAL_GROUP_FORM_TABS;

  const updateField = (key, value) => {
    setFormData((prev) => ({ ...prev, [key]: value }));
    if (fieldError) setFieldError('');
  };

  const updateModelMappingRow = (index, key, value) => {
    setFormData((prev) => ({
      ...prev,
      modelMappingRows: (prev.modelMappingRows || []).map((row, rowIndex) => (
        rowIndex === index ? { ...row, [key]: value } : row
      ))
    }));
    if (fieldError) setFieldError('');
  };

  const addModelMappingRow = () => {
    setFormData((prev) => ({
      ...prev,
      modelMappingRows: [...(prev.modelMappingRows || []), { sourceModel: '', targetModel: '' }]
    }));
  };

  const removeModelMappingRow = (index) => {
    setFormData((prev) => {
      const rows = (prev.modelMappingRows || []).filter((_, rowIndex) => rowIndex !== index);
      return { ...prev, modelMappingRows: rows.length > 0 ? rows : [{ sourceModel: '', targetModel: '' }] };
    });
  };

  const handleStandardMultiplierSave = (values) => {
    setFormData((current) => ({ ...current, ...values }));
    setStandardMultiplierSettingsOpen(false);
    if (fieldError) setFieldError('');
  };

  const handleLongContextToggle = () => {
    if (loading) return;
    if (formData.disableLongContextPricing) {
      setLongContextSettingsOpen(true);
      return;
    }
    updateField('disableLongContextPricing', true);
  };

  const handleLongContextSave = (values) => {
    setFormData((current) => ({ ...current, ...values, disableLongContextPricing: false }));
    setLongContextSettingsOpen(false);
    if (fieldError) setFieldError('');
  };

  const updateCopyEndpointsEnabled = (enabled) => {
    setCopyEndpointsEnabled(enabled);
    if (enabled) {
      setActiveTab('copy');
      return;
    }
    updateField('copyEndpointsFromGroupId', '');
    if (activeTab === 'copy') setActiveTab('basic');
  };

  const updateCopySource = (value) => {
    updateField('copyEndpointsFromGroupId', value);
    if (!value) updateCopyEndpointsEnabled(false);
  };

  const updateModelMultiplierRows = (rows) => {
    setFormData((prev) => ({ ...prev, modelMultiplierRows: rows }));
    if (fieldError) setFieldError('');
  };

	const updateBillingMode = (billingMode) => {
	  setFormData((prev) => ({
		...prev,
		billingMode,
		billingRuleRows: billingMode === 'rules' && prev.billingRuleRows.length === 0
		  ? [createDefaultBillingRuleRow(prev)]
		  : prev.billingRuleRows
	  }));
	  if (fieldError) setFieldError('');
	};

	const updateBillingRuleRows = (billingRuleRows) => {
	  setFormData((prev) => ({ ...prev, billingRuleRows }));
	  if (fieldError) setFieldError('');
	};

  const buildSubmitPayload = () => {
    const name = formData.name.trim();
    const apiKey = formData.authMode === 'client_passthrough' ? '' : formData.apiKey.trim();
    const subscriptionError = validateSubscription(formData);
    if (subscriptionError) { setFieldError(subscriptionError); setFormStep('details'); setActiveTab('basic'); return null; }
    if (!name) {
      setFieldError('分组名称不能为空');
      setFormStep('details');
      setActiveTab('basic');
      return null;
    }
    if (formData.authMode !== 'client_passthrough' && !apiKey) {
      setFieldError('API Key 不能为空');
      setFormStep('details');
      setActiveTab('basic');
      return null;
    }
	const modelPolicy = ['inherit', 'override', 'passthrough'].includes(formData.modelPolicy)
	  ? formData.modelPolicy
	  : 'inherit';
	const dailyCostLimitUSD = Number(formData.dailyCostLimitUSD);
	if (!Number.isFinite(dailyCostLimitUSD) || dailyCostLimitUSD < 0) {
	  setFieldError('每日成本限额必须是大于或等于 0 的数字');
	  setFormStep('details');
	  setActiveTab('billing');
	  return null;
	}
    const multiplierResult = parseEndpointMultiplierPayload({
      costMultiplier: formData.costMultiplier,
      inputCostMultiplier: formData.inputCostMultiplier,
      outputCostMultiplier: formData.outputCostMultiplier,
      cacheCreationCostMultiplier: formData.cacheCreationCostMultiplier,
      cacheReadCostMultiplier: formData.cacheReadCostMultiplier,
      longContextCostMultiplier: formData.longContextCostMultiplier,
      longContextInputCostMultiplier: formData.longContextInputCostMultiplier,
      longContextOutputCostMultiplier: formData.longContextOutputCostMultiplier,
      longContextCacheCreationCostMultiplier: formData.longContextCacheCreationCostMultiplier,
      longContextCacheReadCostMultiplier: formData.longContextCacheReadCostMultiplier
    });
    if (multiplierResult.error) {
      setFieldError(multiplierResult.error);
      setFormStep('details');
      setActiveTab('billing');
      return null;
    }
    const modelMultiplierResult = parseModelMultiplierRows(formData.modelMultiplierRows);
    if (modelMultiplierResult.error) {
      setFieldError(modelMultiplierResult.error);
      setFormStep('details');
      setActiveTab('billing');
      return null;
    }
	let billingRules = [];
	if (formData.billingMode === 'rules') {
	  const billingRuleResult = parseBillingRuleRows(formData.billingRuleRows);
	  if (billingRuleResult.error) {
		setFieldError(billingRuleResult.error);
		setFormStep('details');
		setActiveTab('billing');
		return null;
	  }
	  billingRules = billingRuleResult.payload;
	}
    return {
      name,
      authMode: formData.authMode, subscription: formData.subscription,
      apiKey,
      isDefault: formData.isDefault,
	  modelPolicy,
	  modelMapping: modelPolicy === 'override' ? buildModelMappingPayload(formData.modelMappingRows) : {},
	  supportedModels: String(formData.supportedModelsText || '').split(',').map((model) => model.trim()).filter(Boolean),
	  dailyCostLimitUSD,
      copyEndpointsFromGroupId: formData.authMode === 'client_passthrough' ? '' : (formData.copyEndpointsFromGroupId || ''),
      ...multiplierResult.payload,
      disableLongContextPricing: formData.disableLongContextPricing,billingOutputTokenSource: formData.billingOutputTokenSource,
	  modelMultipliers: modelMultiplierResult.payload,
	  billingRules
    };
  };

  const handleSubmit = async (event) => {
    event.preventDefault();
    const payload = buildSubmitPayload();
    if (!payload) return;
    if (!isEdit && copyEndpointsEnabled && !selectedCopyOption?.value) {
      setFieldError('请选择复制来源分组');
      setFormStep('details');
      setActiveTab('copy');
      return;
    }
    if (!isEdit && selectedCopyOption?.value && selectedCopyOption.endpoints?.length > 0) {
      setCopyStepEndpoints(selectedCopyOption.endpoints);
      setFormStep('copyNames');
      setFieldError('');
      return;
    }
    await onSave?.({ ...payload, copyEndpoints: [] });
  };

  const handleConfirmCopyNames = async (editedEndpoints) => {
    const payload = buildSubmitPayload();
    if (!payload) return;
    await onSave?.({ ...payload, copyEndpoints: editedEndpoints });
  };

  return renderModalPortal(
    <div
      className="fixed inset-0 z-[100] flex items-center justify-center overflow-hidden bg-slate-900/30 p-4 backdrop-blur-sm animate-fade-in"
    >
      <div
        className="flex max-h-[calc(100vh-2rem)] w-full max-w-2xl flex-col overflow-hidden rounded-2xl bg-white shadow-xl"
        style={{ maxHeight: modalMaxHeight }}
      >
        <div className="flex items-start justify-between gap-4 border-b border-slate-100 px-6 py-4">
          <div className="flex items-start gap-3">
            <div className="rounded-xl bg-slate-900 p-2 text-white shadow-sm">
              <KeyRound size={18} />
            </div>
            <div>
              <h2 className="text-lg font-semibold text-slate-900">{isEdit ? '编辑凭证分组' : '添加凭证分组'}</h2>
              <p className="mt-0.5 text-xs text-slate-500">渠道：{channel?.name}</p>
            </div>
          </div>
          <Tooltip content="关闭">
            <button
              type="button"
              onClick={onCancel}
              className="rounded-lg p-2 text-slate-400 transition-colors hover:bg-slate-100 hover:text-slate-700"
              disabled={loading}
            >
              <XCircle size={18} />
            </button>
          </Tooltip>
        </div>

        <form className="min-h-0 flex-1 overflow-y-auto" onSubmit={handleSubmit} autoComplete="off">
          {formStep === 'copyNames' ? (
            <EndpointCopyNameEditorStep
              endpoints={copyStepEndpoints}
              description={`确认后将创建分组「${formData.name.trim() || '新分组'}」并复制 ${copyStepEndpoints.length} 个端点`}
              loading={loading}
              error={serverError}
              onCancel={onCancel}
              onBack={() => {
                setFormStep('details');
                setActiveTab('copy');
              }}
              onConfirm={handleConfirmCopyNames}
            />
          ) : (
            <>
              <div className="border-b border-slate-100 bg-slate-50/70 px-6 py-3">
                <SegmentedTabs
                  tabs={credentialGroupTabs}
                  activeTab={activeTab}
                  onChange={setActiveTab}
                  disabled={loading}
                  className={credentialGroupTabs.length === 4 ? 'grid-cols-4' : credentialGroupTabs.length === 3 ? 'grid-cols-3' : 'grid-cols-2'}
                  buttonPaddingClass="px-2"
                />
              </div>

              <div className="px-6 py-5">
            {(fieldError || serverError) && (
              <div className="mb-4 rounded-lg border border-rose-200 bg-rose-50 px-4 py-3 text-sm text-rose-700">
                {fieldError || serverError}
              </div>
            )}

            {activeTab === 'basic' && (
              <div className="space-y-4">
                <Input
                  label="分组名称"
                  value={formData.name}
                  onChange={(event) => updateField('name', event.target.value)}
                  placeholder="例如：主账号 / 高倍率组"
                  autoFocus
                  disabled={loading}
                />

                <div><label className="mb-1.5 block text-sm font-medium text-slate-700">认证方式</label><CustomSelect value={formData.authMode} options={[{ value: 'group_credential', label: '分组凭证' }, { value: 'client_passthrough', label: '官方订阅' }]} onChange={(authMode) => updateField('authMode', authMode)} disabled={loading} /></div>
                {formData.authMode === 'client_passthrough' ? <SubscriptionSettings provider="codex" value={formData.subscription} onChange={(value) => updateField('subscription', value)} disabled={loading} /> : (
                <div>
                  <label className="mb-1.5 block text-sm font-medium text-slate-700">API Key</label>
                  <div className="relative">
                    <input
                      type={showApiKey ? 'text' : 'password'}
                      value={formData.apiKey}
                      onChange={(event) => updateField('apiKey', event.target.value)}
                      placeholder={isEdit && group?.apiKeyMasked ? group.apiKeyMasked : 'sk-...'}
                      className="w-full rounded-lg border border-slate-200 px-3 py-2 pr-10 font-mono text-sm focus:border-indigo-500 focus:outline-none focus:ring-2 focus:ring-indigo-500 [&::-ms-reveal]:hidden [&::-ms-clear]:hidden [&::-webkit-credentials-auto-fill-button]:hidden"
                      disabled={loading}
                    />
                    <button
                      type="button"
                      onClick={() => setShowApiKey((prev) => !prev)}
                      className="absolute right-2 top-1/2 -translate-y-1/2 rounded-md p-1 text-slate-400 hover:bg-slate-100 hover:text-slate-700"
                      tabIndex={-1}
                    >
                      {showApiKey ? <EyeOff size={16} /> : <Eye size={16} />}
                    </button>
                  </div>
                </div>

                )}
                <div className={`grid gap-3 ${canCopyEndpoints ? 'md:grid-cols-2' : ''}`}>
                  <button
                    type="button"
                    onClick={() => !loading && updateField('isDefault', !formData.isDefault)}
                    className={`flex min-h-[68px] w-full items-center justify-between gap-3 rounded-xl border border-slate-200 bg-slate-50 p-3 text-left transition-all duration-200 ${
                      loading ? 'cursor-not-allowed opacity-70' : 'hover:border-slate-300'
                    }`}
                    disabled={loading}
                  >
                    <span className={`text-sm font-medium ${formData.isDefault ? 'text-indigo-700' : 'text-slate-600'}`}>
                      设为渠道默认分组
                    </span>
                    <div className={`relative h-5 w-10 rounded-full transition-colors duration-200 ${
                      formData.isDefault ? 'bg-indigo-500' : 'bg-slate-300'
                    }`}>
                      <div className={`absolute left-0.5 top-0.5 h-4 w-4 rounded-full bg-white shadow-sm transition-transform duration-200 ${
                      formData.isDefault ? 'translate-x-5' : 'translate-x-0'
                      }`} />
                    </div>
                  </button>

                  {canCopyEndpoints && (
                    <button
                      type="button"
                      onClick={() => !loading && updateCopyEndpointsEnabled(!copyEndpointsEnabled)}
                      className={`flex min-h-[68px] w-full items-center justify-between gap-3 rounded-xl border border-slate-200 bg-slate-50 p-3 text-left transition-all duration-200 ${
                        loading ? 'cursor-not-allowed opacity-70' : 'hover:border-slate-300'
                      }`}
                      disabled={loading}
                    >
                      <span className={`text-sm font-medium ${copyEndpointsEnabled ? 'text-indigo-700' : 'text-slate-600'}`}>
                        复制其他分组端点
                      </span>
                      <div className={`relative h-5 w-10 rounded-full transition-colors duration-200 ${
                        copyEndpointsEnabled ? 'bg-indigo-500' : 'bg-slate-300'
                      }`}>
                        <div className={`absolute left-0.5 top-0.5 h-4 w-4 rounded-full bg-white shadow-sm transition-transform duration-200 ${
                          copyEndpointsEnabled ? 'translate-x-5' : 'translate-x-0'
                        }`} />
                      </div>
                    </button>
                  )}
                </div>
              </div>
            )}

            {activeTab === 'model' && (
              <div className="space-y-4">
                <div>
                  <label className="mb-1.5 block text-sm font-medium text-slate-700">模型策略</label>
                  <CustomSelect
                    options={[
                      { value: 'inherit', label: '跟随渠道' },
                      { value: 'override', label: '分组自定义映射' },
                      { value: 'passthrough', label: '原模型直传' }
                    ]}
                    value={formData.modelPolicy}
                    onChange={(value) => updateField('modelPolicy', value)}
                    size="md"
                    fullWidth
                    disabled={loading}
                  />
                  <p className="mt-1 text-xs text-slate-400">
                    选择“分组自定义映射”后，只使用本分组映射，不回退到渠道模型；“原模型直传”会跳过渠道级模型改写。
                  </p>
                </div>

                {formData.modelPolicy === 'override' && (
                  <div className="space-y-3 rounded-xl border border-slate-200 bg-slate-50/60 p-3">
                    <div className="flex items-center justify-between gap-3">
                      <div>
                        <div className="text-sm font-medium text-slate-700">分组模型映射</div>
                        <div className="mt-0.5 text-xs text-slate-500">请求模型精确命中后替换为本分组支持的模型。</div>
                      </div>
                      <Button type="button" variant="secondary" size="sm" onClick={addModelMappingRow} disabled={loading} icon={Plus}>
                        添加
                      </Button>
                    </div>
                    {(formData.modelMappingRows || []).map((row, index) => (
                      <div key={`codex-group-model-mapping-${index}`} className="grid grid-cols-1 gap-2 sm:grid-cols-[1fr_auto_1fr_auto] sm:items-center">
                        <input
                          type="text"
                          value={row.sourceModel}
                          onChange={(event) => updateModelMappingRow(index, 'sourceModel', event.target.value)}
                          placeholder="gpt-5.4-mini"
                          className="min-w-0 rounded-lg border border-slate-200 bg-white px-3 py-2 font-mono text-sm focus:border-indigo-500 focus:outline-none focus:ring-2 focus:ring-indigo-500"
                          disabled={loading}
                        />
                        <ArrowRight size={16} className="text-slate-400" />
                        <input
                          type="text"
                          value={row.targetModel}
                          onChange={(event) => updateModelMappingRow(index, 'targetModel', event.target.value)}
                          placeholder="gpt-5-mini"
                          className="min-w-0 rounded-lg border border-slate-200 bg-white px-3 py-2 font-mono text-sm focus:border-indigo-500 focus:outline-none focus:ring-2 focus:ring-indigo-500"
                          disabled={loading}
                        />
                        <Tooltip content="删除映射">
                          <button type="button" onClick={() => removeModelMappingRow(index)} className="rounded-lg p-2 text-slate-400 hover:bg-rose-50 hover:text-rose-600" disabled={loading}>
                            <Trash2 size={16} />
                          </button>
                        </Tooltip>
                      </div>
                    ))}
                  </div>
                )}

                <div>
                  <label className="mb-1.5 block text-sm font-medium text-slate-700">支持的原始模型（可选）</label>
                  <input
                    type="text"
                    value={formData.supportedModelsText}
                    onChange={(event) => updateField('supportedModelsText', event.target.value)}
                    placeholder="gpt-5, gpt-5-mini，用逗号分隔"
                    className="w-full rounded-lg border border-slate-200 px-3 py-2 font-mono text-sm focus:border-indigo-500 focus:outline-none focus:ring-2 focus:ring-indigo-500"
                    disabled={loading}
                  />
                  <p className="mt-1 text-xs text-slate-400">留空表示不声明能力；填写后，既未命中映射也不在列表中的请求会跳过该分组。</p>
                </div>
              </div>
            )}

            {activeTab === 'copy' && canCopyEndpoints && copyEndpointsEnabled && (
              <div className="space-y-4">
                <div className={`rounded-xl border border-slate-200 bg-slate-50 p-3 transition-all duration-200 ${
                  loading ? 'opacity-70' : 'hover:border-slate-300'
                }`}>
                  <div className="flex items-center gap-2">
                    <span className="shrink-0 text-xs font-medium text-slate-500">复制来源</span>
                    <div className="min-w-0 flex-1">
                      <EndpointCopySourcePicker
                        options={copyChoices}
                        value={formData.copyEndpointsFromGroupId || ''}
                        onChange={updateCopySource}
                        disabled={loading}
                      />
                    </div>
                  </div>
                </div>
              </div>
            )}

            {activeTab === 'billing' && (
              <div className="space-y-4">
				<div className="rounded-xl border border-indigo-100 bg-indigo-50/40 p-3">
				  <label className="mb-1.5 block text-sm font-medium text-slate-700">每日成本限额（USD）</label>
				  <input
					type="number"
					min="0"
					step="0.01"
					value={formData.dailyCostLimitUSD}
					onChange={(event) => updateField('dailyCostLimitUSD', event.target.value)}
					className="w-full rounded-lg border border-slate-200 bg-white px-3 py-2 text-sm focus:border-indigo-500 focus:outline-none focus:ring-2 focus:ring-indigo-500/20"
					disabled={loading}
				  />
				  <p className="mt-1 text-xs text-slate-500">0 表示不限额；达到限额后自动切换到其他凭证分组。</p>
				</div>
				<div>
				  <div className="mb-1.5 flex items-baseline justify-between gap-2">
					<label className="text-sm font-medium text-slate-700">计费模式</label>
					<span className="text-xs text-slate-400">仅所选模式生效</span>
				  </div>
				  <div role="radiogroup" aria-label="计费模式" className="grid grid-cols-2 gap-2">
					{CODEX_BILLING_MODES.map((mode) => {
					  const selected = formData.billingMode === mode.key;
					  return (
						<button
						  key={mode.key}
						  type="button"
						  role="radio"
						  aria-checked={selected}
						  onClick={() => updateBillingMode(mode.key)}
						  disabled={loading}
						  className={`flex items-start gap-2.5 rounded-xl border p-3 text-left transition-colors ${
							selected ? 'border-indigo-300 bg-indigo-50/60' : 'border-slate-200 bg-white'
						  } ${loading ? 'cursor-not-allowed opacity-70' : 'hover:border-indigo-300'}`}
						>
						  <span className={`mt-0.5 flex h-4 w-4 shrink-0 items-center justify-center rounded-full border-2 transition-colors ${
							selected ? 'border-indigo-600' : 'border-slate-300'
						  }`}>
							{selected && <span className="h-2 w-2 rounded-full bg-indigo-600" />}
						  </span>
						  <span className="min-w-0">
							<span className={`block text-sm font-medium ${selected ? 'text-indigo-700' : 'text-slate-700'}`}>{mode.label}</span>
							<span className="mt-0.5 block text-xs text-slate-500">{mode.description}</span>
						  </span>
						</button>
					  );
					})}
				  </div>
				</div>

				<div>
				  <label className="mb-1.5 block text-sm font-medium text-slate-700">默认输出计费口径</label>
				  <CustomSelect
					options={BILLING_OUTPUT_TOKEN_SOURCE_OPTIONS}
					value={formData.billingOutputTokenSource}
					onChange={(value) => updateField('billingOutputTokenSource', value)}
					size="md"
					fullWidth
				  />
				</div>

				{formData.billingMode === 'legacy' ? (
				  <>
                <div className="rounded-xl border border-slate-200 bg-slate-50/60 p-3">
                  <div className="flex items-center justify-between gap-3">
                    <div>
                      <div className="text-sm font-medium text-slate-700">普通成本倍率</div>
                      <div className="mt-0.5 text-xs text-slate-500">未命中 Long context 阈值时使用以下倍率</div>
                    </div>
                    <Button type="button" variant="secondary" size="sm" onClick={() => setStandardMultiplierSettingsOpen(true)} disabled={loading}>编辑</Button>
                  </div>
                  <div className="mt-3 grid grid-cols-2 gap-2 sm:grid-cols-5">
                    {STANDARD_ENDPOINT_MULTIPLIER_FIELDS.map(({ key, label }) => (
                      <div key={key} className="rounded-lg border border-slate-200 bg-white px-2.5 py-2">
                        <div className="truncate text-[11px] text-slate-400">{label.replace(/成本倍率|倍率/, '') || '总'}</div>
                        <div className="mt-0.5 truncate text-sm font-medium text-slate-700">{formData[key]}</div>
                      </div>
                    ))}
                  </div>
                  <p className="mt-2 text-xs text-slate-400">成本倍率直接作为该分组下 Codex 端点的计费倍率，{MULTIPLIER_EXPRESSION_HELP}</p>
                </div>

                {standardMultiplierSettingsOpen && (
                  <MultiplierSettingsModal
                    title="普通成本倍率"
                    description="未命中 Long context 阈值时应用以下倍率。"
                    fields={STANDARD_ENDPOINT_MULTIPLIER_FIELDS}
                    values={formData}
                    disabled={loading}
                    onCancel={() => setStandardMultiplierSettingsOpen(false)}
                    onSave={handleStandardMultiplierSave}
                  />
                )}

                <button
                  type="button"
                  role="switch"
                  aria-checked={!formData.disableLongContextPricing}
                  onClick={handleLongContextToggle}
                  className={`flex w-full items-center justify-between gap-3 rounded-xl border p-3 text-left transition-colors ${
                    formData.disableLongContextPricing
                      ? 'border-slate-200 bg-slate-50'
                      : 'border-indigo-200 bg-indigo-50/60'
                  } ${loading ? 'cursor-not-allowed opacity-70' : 'hover:border-indigo-300'}`}
                  disabled={loading}
                >
                  <div>
                    <div className="text-sm font-medium text-slate-700">Long context 计费</div>
                    <div className="mt-0.5 text-xs text-slate-500">
                      {formData.disableLongContextPricing ? '不启用，始终使用普通模型价格' : '跟随模型定价，命中阈值后应用 Long context 倍率'}
                    </div>
                  </div>
                  <span className={`relative inline-flex h-6 w-11 shrink-0 rounded-full transition-colors ${
                    formData.disableLongContextPricing ? 'bg-slate-300' : 'bg-indigo-600'
                  }`}>
                    <span className={`absolute top-0.5 h-5 w-5 rounded-full bg-white shadow transition-transform ${
                      formData.disableLongContextPricing ? 'translate-x-0.5' : 'translate-x-[1.375rem]'
                    }`} />
                  </span>
                </button>

                {!formData.disableLongContextPricing && (
                  <div className="rounded-xl border border-slate-200 bg-slate-50/60 p-3">
                    <div className="flex items-center justify-between gap-3">
                      <div>
                        <div className="text-sm font-medium text-slate-700">Long context 倍率已配置</div>
                        <div className="mt-0.5 text-xs text-slate-500">命中阈值后使用以下倍率</div>
                      </div>
                      <Button type="button" variant="secondary" size="sm" onClick={() => setLongContextSettingsOpen(true)} disabled={loading}>编辑</Button>
                    </div>
                    <div className="mt-3 grid grid-cols-2 gap-2 sm:grid-cols-5">
                      {LONG_CONTEXT_ENDPOINT_MULTIPLIER_FIELDS.map(({ key, label }) => (
                        <div key={key} className="rounded-lg border border-slate-200 bg-white px-2.5 py-2">
                          <div className="truncate text-[11px] text-slate-400">{label.replace('Long context ', '')}</div>
                          <div className="mt-0.5 truncate text-sm font-medium text-slate-700">{formData[key]}</div>
                        </div>
                      ))}
                    </div>
                  </div>
                )}

                {longContextSettingsOpen && (
                  <MultiplierSettingsModal
                    title="Long context 成本倍率"
                    description="命中模型 Long context 阈值后应用以下倍率。"
                    fields={LONG_CONTEXT_ENDPOINT_MULTIPLIER_FIELDS}
                    values={formData}
                    disabled={loading}
                    onCancel={() => setLongContextSettingsOpen(false)}
                    onSave={handleLongContextSave}
                  />
                )}


                <ModelMultiplierEditor
                  rows={formData.modelMultiplierRows}
                  onChange={updateModelMultiplierRows}
                  disabled={loading}
                />
				  </>
				) : (
				  <BillingRuleEditor
					rows={formData.billingRuleRows}
					onChange={updateBillingRuleRows}
					disabled={loading}
				  />
				)}
              </div>
            )}

            <div className="mt-5 flex justify-end gap-3 border-t border-slate-100 pt-4">
              <Button type="button" variant="ghost" onClick={onCancel} disabled={loading}>取消</Button>
              <Button type="submit" variant="primary" loading={loading}>
                {isEdit ? '保存分组' : (continueToEndpoint || copyEndpointsEnabled ? '下一步' : '添加分组')}
              </Button>
            </div>
              </div>
            </>
          )}
        </form>
      </div>
    </div>
  );
};

export { CredentialGroupFormModal };
