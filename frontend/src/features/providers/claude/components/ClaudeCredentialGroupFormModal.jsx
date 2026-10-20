import { useState } from 'react';
import { Plus, Trash2, ArrowRight, XCircle, KeyRound, Eye, EyeOff } from 'lucide-react';
import { Button, CustomSelect, SegmentedTabs, Tooltip } from '@/shared/ui/index.js';
import { renderModalPortal } from '@/shared/ui/modalPortal.js';
import SubscriptionSettings from './SubscriptionSettings.jsx';
import { validateSubscription } from '../utils/subscription.js';
import ModelMultiplierEditor from '@/features/providers/shared/billing/ModelMultiplierEditor.jsx';
import EndpointCopyNameEditorStep, { EndpointCopySourcePicker } from '@/features/providers/shared/endpoints/EndpointCopyNameEditorStep.jsx';
import { buildModelMappingPayload } from '@/features/providers/shared/routing/modelMapping.js';
import { MULTIPLIER_EXPRESSION_HELP, parseModelMultiplierRows } from '@/features/providers/shared/billing/modelMultiplierRows.js';
import MultiplierSettingsModal from '@/features/providers/shared/billing/MultiplierSettingsModal.jsx';
import { CREDENTIAL_GROUP_FORM_TABS, CREDENTIAL_GROUP_CREATE_FORM_TABS, CLAUDE_CREDENTIAL_SECRET_TABS, getInitialClaudeCredentialSecretType, STANDARD_CREDENTIAL_GROUP_MULTIPLIER_FIELDS, buildInitialClaudeCredentialGroupForm, validateClaudeCredentialGroupMultipliers } from '../model.js';

const ClaudeCredentialGroupFormModal = ({
  open,
  mode = 'create',
  channelName = '',
  group = null,
  continueToEndpoint = false,
  loading = false,
  serverError = '',
  endpointCopyOptions = [],
  channels = [],
  onClose,
  onSubmit
}) => {
  const isEdit = mode === 'edit';
  const [formData, setFormData] = useState(() => buildInitialClaudeCredentialGroupForm(group, channelName));
  const [activeTab, setActiveTab] = useState('basic');
  const [standardMultiplierSettingsOpen, setStandardMultiplierSettingsOpen] = useState(false);
  const [error, setError] = useState('');
  const [showToken, setShowToken] = useState(false);
  const [showApiKey, setShowApiKey] = useState(false);
  const [credentialSecretType, setCredentialSecretType] = useState(() => getInitialClaudeCredentialSecretType(group));
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
    ? CREDENTIAL_GROUP_CREATE_FORM_TABS
    : CREDENTIAL_GROUP_FORM_TABS;

  if (!open) return null;

  const updateField = (key, value) => {
    setFormData((prev) => ({ ...prev, [key]: value }));
    if (error) setError('');
  };

  const updateModelMappingRow = (index, key, value) => {
    setFormData((prev) => ({
      ...prev,
      modelMappingRows: (prev.modelMappingRows || []).map((row, rowIndex) => (
        rowIndex === index ? { ...row, [key]: value } : row
      ))
    }));
    if (error) setError('');
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
    setFormData((prev) => ({ ...prev, ...values }));
    setStandardMultiplierSettingsOpen(false);
    if (error) setError('');
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

  const updateCredentialSecretType = (nextType) => {
    setCredentialSecretType(nextType);
    if (error) setError('');
  };

  const updateModelMultiplierRows = (rows) => {
    setFormData((prev) => ({ ...prev, modelMultiplierRows: rows }));
    if (error) setError('');
  };

  const buildSubmitPayload = () => {
    const name = formData.name.trim();
    const token = formData.authMode !== 'client_passthrough' && credentialSecretType === 'token' ? formData.token.trim() : '';
    const apiKey = formData.authMode !== 'client_passthrough' && credentialSecretType === 'apiKey' ? formData.apiKey.trim() : '';

    if (!name) {
      setError('分组名称不能为空');
      setFormStep('details');
      setActiveTab('basic');
      return null;
    }
    if (formData.authMode !== 'client_passthrough' && credentialSecretType === 'token' && !token) {
      setError('Token 不能为空');
      setFormStep('details');
      setActiveTab('basic');
      return null;
    }
    if (formData.authMode !== 'client_passthrough' && credentialSecretType === 'apiKey' && !apiKey) {
      setError('API Key 不能为空');
      setFormStep('details');
      setActiveTab('basic');
      return null;
    }

	const dailyCostLimitUSD = Number(formData.dailyCostLimitUSD);
	if (!Number.isFinite(dailyCostLimitUSD) || dailyCostLimitUSD < 0) {
	  setError('每日成本限额必须是大于或等于 0 的数字');
	  setFormStep('details');
	  setActiveTab('billing');
	  return null;
	}

    const subscriptionError = validateSubscription(formData);
    if (subscriptionError) { setError(subscriptionError); setFormStep('details'); setActiveTab('basic'); return null; }

    const multiplierError = validateClaudeCredentialGroupMultipliers(formData);
    if (multiplierError) {
      setError(multiplierError);
      setFormStep('details');
      setActiveTab('billing');
      return null;
    }

    const modelMultiplierResult = parseModelMultiplierRows(formData.modelMultiplierRows);
    if (modelMultiplierResult.error) {
      setError(modelMultiplierResult.error);
      setFormStep('details');
      setActiveTab('billing');
      return null;
    }

    return {
      ...formData,
      channel: formData.channel || channelName,
      name,
      token,
      apiKey,
	  dailyCostLimitUSD,
      copyEndpointsFromGroupId: formData.authMode === 'client_passthrough' ? '' : (formData.copyEndpointsFromGroupId || ''),
      modelPolicy: ['inherit', 'override', 'passthrough'].includes(formData.modelPolicy) ? formData.modelPolicy : 'inherit',
      modelMapping: formData.modelPolicy === 'override' ? buildModelMappingPayload(formData.modelMappingRows) : {},
      supportedModels: String(formData.supportedModelsText || '').split(',').map((model) => model.trim()).filter(Boolean),
      modelMultipliers: modelMultiplierResult.payload
    };
  };

  const renderSecretInput = ({ label, field, placeholder, visible, setVisible }) => (
    <div>
      <label className="mb-1.5 block text-sm font-medium text-slate-700">{label}</label>
      <div className="relative">
        <input
          type={visible ? 'text' : 'password'}
          value={formData[field] || ''}
          onChange={(event) => updateField(field, event.target.value)}
          placeholder={placeholder}
          autoComplete="new-password"
          data-form-type="other"
          className="w-full rounded-lg border border-slate-200 px-3 py-2 pr-10 font-mono text-sm focus:border-indigo-500 focus:outline-none focus:ring-2 focus:ring-indigo-500/20 [&::-ms-reveal]:hidden [&::-ms-clear]:hidden [&::-webkit-credentials-auto-fill-button]:hidden"
          disabled={loading}
        />
        <Tooltip content={visible ? '隐藏' : '显示'} className="absolute right-2 top-1/2 -translate-y-1/2">
          <button
            type="button"
            onClick={() => setVisible((prev) => !prev)}
            className="rounded-md p-1 text-slate-400 transition-colors hover:bg-slate-100 hover:text-slate-700"
            tabIndex={-1}
            aria-label={visible ? '隐藏' : '显示'}
          >
            {visible ? <EyeOff size={16} /> : <Eye size={16} />}
          </button>
        </Tooltip>
      </div>
    </div>
  );

  const handleSubmit = async (event) => {
    event.preventDefault();
    const payload = buildSubmitPayload();
    if (!payload) return;
    if (!isEdit && copyEndpointsEnabled && !selectedCopyOption?.value) {
      setError('请选择复制来源分组');
      setFormStep('details');
      setActiveTab('copy');
      return;
    }
    if (!isEdit && selectedCopyOption?.value && selectedCopyOption.endpoints?.length > 0) {
      setCopyStepEndpoints(selectedCopyOption.endpoints);
      setFormStep('copyNames');
      setError('');
      return;
    }

    await onSubmit?.({ ...payload, copyEndpoints: [] });
  };

  const handleConfirmCopyNames = async (editedEndpoints) => {
    const payload = buildSubmitPayload();
    if (!payload) return;
    await onSubmit?.({ ...payload, copyEndpoints: editedEndpoints });
  };

  return renderModalPortal(
    <div className="fixed inset-0 z-[60] flex items-center justify-center overflow-hidden bg-slate-900/30 p-4 backdrop-blur-sm animate-fade-in">
      <div className="flex max-h-[calc(100vh-2rem)] w-full max-w-2xl flex-col overflow-hidden rounded-2xl bg-white shadow-xl">
        <div className="flex items-start justify-between gap-4 border-b border-slate-100 px-6 py-4">
          <div className="flex items-start gap-3">
            <div className="rounded-xl bg-slate-900 p-2 text-white shadow-sm">
              <KeyRound size={18} />
            </div>
            <div>
              <h2 className="text-lg font-semibold text-slate-900">{isEdit ? '编辑凭证分组' : '添加凭证分组'}</h2>
              <p className="mt-0.5 text-xs text-slate-500">渠道：{channelName || formData.channel || '-'}</p>
            </div>
          </div>
          <Tooltip content="关闭">
            <button
              type="button"
              onClick={onClose}
              className="rounded-lg p-2 text-slate-400 transition-colors hover:bg-slate-100 hover:text-slate-700"
              disabled={loading}
              aria-label="关闭"
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
              onCancel={onClose}
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
                  className={credentialGroupTabs.length === 4 ? 'grid-cols-4' : 'grid-cols-3'}
                  buttonPaddingClass="px-2"
                />
              </div>

              <div className="px-6 py-5">
            {(error || serverError) && (
              <div className="mb-4 rounded-lg border border-rose-200 bg-rose-50 px-4 py-3 text-sm text-rose-700">
                {error || serverError}
              </div>
            )}

            {activeTab === 'basic' && (
              <div className="space-y-4">
                <div>
                  <label className="mb-1.5 block text-sm font-medium text-slate-700">
                    分组名称 <span className="text-rose-500">*</span>
                  </label>
                  <input
                    value={formData.name}
                    onChange={(event) => updateField('name', event.target.value)}
                    placeholder="例如：主账号 / 高倍率组"
                    autoComplete="off"
                    data-form-type="other"
                    className="w-full rounded-lg border border-slate-200 px-3 py-2 text-sm focus:border-indigo-500 focus:outline-none focus:ring-2 focus:ring-indigo-500/20"
                    disabled={loading}
                    autoFocus
                  />
                </div>

                <div className="space-y-2">
                  <label className="block text-sm font-medium text-slate-700">认证方式</label>
                  <CustomSelect value={formData.authMode} options={[{ value: 'group_credential', label: '分组凭证' }, { value: 'client_passthrough', label: '官方订阅' }]} onChange={(authMode) => updateField('authMode', authMode)} disabled={loading} />
                </div>
                {formData.authMode === 'client_passthrough' ? <SubscriptionSettings value={formData.subscription} onChange={(value) => updateField('subscription', value)} channels={channels} localKeyConfigured={formData.localKeyConfigured} acknowledged={formData.localKeyAcknowledged} onAcknowledge={(value) => updateField('localKeyAcknowledged', value)} disabled={loading} /> : (
                <div className="space-y-3">
                  <SegmentedTabs
                    tabs={CLAUDE_CREDENTIAL_SECRET_TABS}
                    activeTab={credentialSecretType}
                    onChange={updateCredentialSecretType}
                    disabled={loading}
                    className="grid-cols-2"
                    buttonPaddingClass="px-2"
                  />
                  {credentialSecretType === 'token' && renderSecretInput({
                    label: 'Token',
                    field: 'token',
                    placeholder: 'sk-...',
                    visible: showToken,
                    setVisible: setShowToken
                  })}
                  {credentialSecretType === 'apiKey' && renderSecretInput({
                    label: 'API Key',
                    field: 'apiKey',
                    placeholder: 'sk-ant-...',
                    visible: showApiKey,
                    setVisible: setShowApiKey
                  })}
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
                  <p className="mt-1 text-xs text-slate-400">覆盖模式只使用本分组映射；直传模式跳过模型改写。</p>
                </div>

                {formData.modelPolicy === 'override' && (
                  <div className="space-y-3 rounded-xl border border-slate-200 bg-slate-50/60 p-3">
                    <div className="flex items-center justify-between gap-3">
                      <div>
                        <div className="text-sm font-medium text-slate-700">分组模型映射</div>
                        <div className="mt-0.5 text-xs text-slate-500">请求模型命中后替换为本分组目标模型。</div>
                      </div>
                      <Button type="button" variant="secondary" size="sm" onClick={addModelMappingRow} disabled={loading} icon={Plus}>添加</Button>
                    </div>
                    {(formData.modelMappingRows || []).map((row, index) => (
                      <div key={`claude-group-model-mapping-${index}`} className="grid grid-cols-1 gap-2 sm:grid-cols-[1fr_auto_1fr_auto] sm:items-center">
                        <input
                          type="text"
                          value={row.sourceModel}
                          onChange={(event) => updateModelMappingRow(index, 'sourceModel', event.target.value)}
                          placeholder="claude-3-5-haiku-20241022"
                          className="min-w-0 rounded-lg border border-slate-200 bg-white px-3 py-2 font-mono text-sm focus:border-indigo-500 focus:outline-none focus:ring-2 focus:ring-indigo-500/20"
                          disabled={loading}
                        />
                        <ArrowRight size={16} className="text-slate-400" />
                        <input
                          type="text"
                          value={row.targetModel}
                          onChange={(event) => updateModelMappingRow(index, 'targetModel', event.target.value)}
                          placeholder="claude-sonnet-4-20250514"
                          className="min-w-0 rounded-lg border border-slate-200 bg-white px-3 py-2 font-mono text-sm focus:border-indigo-500 focus:outline-none focus:ring-2 focus:ring-indigo-500/20"
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
                    placeholder="claude-sonnet-4, claude-haiku-4，用逗号分隔"
                    className="w-full rounded-lg border border-slate-200 px-3 py-2 font-mono text-sm focus:border-indigo-500 focus:outline-none focus:ring-2 focus:ring-indigo-500/20"
                    disabled={loading}
                  />
                  <p className="mt-1 text-xs text-slate-400">留空表示不声明能力；填写后，不匹配的请求会跳过该分组。</p>
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
                <div className="rounded-xl border border-slate-200 bg-slate-50/60 p-3">
                  <div className="flex items-center justify-between gap-3">
                    <div>
                      <div className="text-sm font-medium text-slate-700">普通成本倍率</div>
                      <div className="mt-0.5 text-xs text-slate-500">该分组下 Claude 端点的默认计费倍率</div>
                    </div>
                    <Button type="button" variant="secondary" size="sm" onClick={() => setStandardMultiplierSettingsOpen(true)} disabled={loading}>编辑</Button>
                  </div>
                  <div className="mt-3 grid grid-cols-2 gap-2 sm:grid-cols-3">
                    {STANDARD_CREDENTIAL_GROUP_MULTIPLIER_FIELDS.map((field) => (
                      <div key={field.name} className="rounded-lg border border-slate-200 bg-white px-2.5 py-2">
                        <div className="truncate text-[11px] text-slate-400">{field.label.replace(/成本倍率|倍率/, '') || '总'}</div>
                        <div className="mt-0.5 truncate text-sm font-medium text-slate-700">{formData[field.name]}</div>
                      </div>
                    ))}
                  </div>
                  <p className="mt-2 text-xs text-slate-400">成本倍率会作为该分组下 Claude 端点的默认计费倍率，{MULTIPLIER_EXPRESSION_HELP}</p>
                </div>

                {standardMultiplierSettingsOpen && (
                  <MultiplierSettingsModal
                    title="普通成本倍率"
                    description="该分组下 Claude 端点的默认计费倍率。"
                    fields={STANDARD_CREDENTIAL_GROUP_MULTIPLIER_FIELDS}
                    values={formData}
                    pricingProvider="claude"
                    disabled={loading}
                    onCancel={() => setStandardMultiplierSettingsOpen(false)}
                    onSave={handleStandardMultiplierSave}
                  />
                )}

                <ModelMultiplierEditor
                  rows={formData.modelMultiplierRows}
                  onChange={updateModelMultiplierRows}
                  disabled={loading}
                />
              </div>
            )}

            <div className="mt-5 flex justify-end gap-3 border-t border-slate-100 pt-4">
              <Button type="button" variant="ghost" onClick={onClose} disabled={loading}>
                取消
              </Button>
              <Button type="submit" variant="primary" loading={loading}>
                {continueToEndpoint ? '下一步' : isEdit ? '保存分组' : copyEndpointsEnabled ? '下一步' : '添加分组'}
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

export { ClaudeCredentialGroupFormModal };
