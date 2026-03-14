// ============================================
// Codex 页面 - Provider 配置接管管理
// 2026-03-10
// ============================================

import { useCallback, useEffect, useState } from 'react';
import {
  ArrowRightLeft,
  CheckCircle2,
  Copy,
  Database,
  Eye,
  EyeOff,
  Globe,
  KeyRound,
  Pencil,
  Plus,
  RefreshCw,
  Server,
  ShieldCheck,
  Trash2,
  TriangleAlert,
  XCircle
} from 'lucide-react';
import {
  Button,
  CustomSelect,
  EmptyState,
  ErrorMessage,
  Input,
  KPICard,
  LoadingSpinner
} from '@components/ui';
import {
  activateCodexProvider,
  createCodexProvider,
  deleteCodexProviderByID,
  getCodexProviders,
  getCodexProviderStorageStatus,
  isWailsEnvironment,
  openExternalURL,
  subscribeToEvent,
  updateCodexProviderByID
} from '@utils/wailsApi.js';

const PROVIDER_TYPE_OPTIONS = [
  { value: 'openai-compatible', label: 'OpenAI 兼容' },
  { value: 'azure-openai', label: 'Azure OpenAI' },
  { value: 'custom', label: '自定义' }
];

const REASONING_EFFORT_OPTIONS = [
  { value: 'low', label: 'low', hint: 'Fastest' },
  { value: 'medium', label: 'medium', hint: 'Balanced' },
  { value: 'high', label: 'high', hint: 'Deeper' },
  { value: 'xhigh', label: 'xhigh', hint: 'Max' }
];
const MODEL_CONTEXT_WINDOW_1M = 1000000;

const getErrorMessage = (error, fallback = '操作失败') => {
  if (!error) return fallback;
  if (typeof error === 'string') return error || fallback;
  if (error instanceof Error) return error.message || fallback;
  if (typeof error?.message === 'string' && error.message) return error.message;
  try {
    return JSON.stringify(error);
  } catch {
    return String(error);
  }
};

const formatProviderType = (providerType) => {
  const matched = PROVIDER_TYPE_OPTIONS.find((option) => option.value === providerType);
  return matched?.label || providerType || '未设置';
};

const formatDateTime = (value) => {
  if (!value) return '-';

  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return String(value);
  }

  return date.toLocaleString('zh-CN', {
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit'
  });
};

const buildInitialForm = (provider) => ({
  name: provider?.name || '',
  providerType: provider?.providerType || 'openai-compatible',
  baseURL: provider?.baseURL || '',
  apiKey: provider?.apiKey || '',
  model: provider?.model || '',
  reasoningEffort: provider?.reasoningEffort || 'xhigh',
  enable1MContext: Boolean(provider?.enable1MContext),
  website: provider?.website || ''
});

const InfoTile = ({ title, value, icon: Icon, mono = false, titleAttr = '' }) => (
  <div className="rounded-xl border border-slate-100 bg-slate-50/80 p-3">
    <div className="text-[11px] font-semibold uppercase tracking-[0.14em] text-slate-400">{title}</div>
    <div
      className={`mt-2 flex items-center gap-2 text-sm text-slate-700 ${mono ? 'font-mono' : ''}`}
      title={titleAttr || value}
    >
      {Icon && <Icon size={14} className="flex-shrink-0 text-slate-400" />}
      <span className="truncate">{value || '-'}</span>
    </div>
  </div>
);

const FormSelectField = ({ label, value, options, onChange, help, placeholder = '请选择' }) => (
  <div className="flex flex-col">
    <label className="mb-1.5 text-sm font-medium text-slate-700">{label}</label>
    <CustomSelect
      options={options}
      value={value}
      onChange={onChange}
      placeholder={placeholder}
      size="md"
      fullWidth
      className="w-full"
    />
    {help && <p className="mt-1 text-xs text-slate-400">{help}</p>}
  </div>
);

const ReasoningEffortSelector = ({ value, onChange }) => (
  <div className="flex flex-col">
    <label className="mb-1.5 text-sm font-medium text-slate-700">推理强度</label>
    <div className="grid grid-cols-2 gap-2">
      {REASONING_EFFORT_OPTIONS.map((option) => {
        const isSelected = option.value === value;
        return (
          <button
            key={option.value}
            type="button"
            onClick={() => onChange?.(option.value)}
            className={`rounded-xl border px-3 py-3 text-left transition-all ${
              isSelected
                ? 'border-amber-300 bg-amber-50 text-amber-900 shadow-sm'
                : 'border-slate-200 bg-white text-slate-600 hover:border-slate-300 hover:bg-slate-50'
            }`}
          >
            <div className="flex items-center justify-between gap-2">
              <span className="font-mono text-sm font-semibold">{option.label}</span>
              <span
                className={`rounded-full px-2 py-0.5 text-[10px] font-semibold uppercase tracking-[0.12em] ${
                  isSelected ? 'bg-amber-100 text-amber-700' : 'bg-slate-100 text-slate-400'
                }`}
              >
                {option.hint}
              </span>
            </div>
          </button>
        );
      })}
    </div>
  </div>
);

const FormToggleField = ({ label, checked, onChange, help, badge = '1M' }) => (
  <div className="flex items-start justify-between gap-4 rounded-2xl border border-slate-200 bg-slate-50/70 px-4 py-3 sm:col-span-2">
    <div className="min-w-0">
      <div className="flex items-center gap-2">
        <label className="text-sm font-medium text-slate-700">{label}</label>
        <span
          className={`rounded-full px-2 py-0.5 text-[10px] font-semibold uppercase tracking-[0.12em] ${
            checked ? 'bg-emerald-100 text-emerald-700' : 'bg-slate-200 text-slate-500'
          }`}
        >
          {checked ? badge : '默认'}
        </span>
      </div>
      {help && <p className="mt-1 text-xs leading-5 text-slate-500">{help}</p>}
    </div>

    <button
      type="button"
      role="switch"
      aria-checked={checked}
      onClick={() => onChange?.(!checked)}
      className={`relative inline-flex h-6 w-11 flex-shrink-0 items-center rounded-full transition-colors focus:outline-none focus-visible:ring-2 focus-visible:ring-emerald-500 focus-visible:ring-offset-2 ${
        checked ? 'bg-emerald-500' : 'bg-slate-300 hover:bg-slate-400'
      }`}
      title={checked ? '关闭 1M 上下文窗口' : '开启 1M 上下文窗口'}
    >
      <span
        className={`inline-block h-5 w-5 transform rounded-full bg-white shadow transition-transform ${
          checked ? 'translate-x-5' : 'translate-x-0.5'
        }`}
      />
    </button>
  </div>
);

const ProviderFormModal = ({ mode, provider, loading, serverError, onCancel, onSave }) => {
  const [formData, setFormData] = useState(() => buildInitialForm(provider));
  const [fieldError, setFieldError] = useState('');
  const [showApiKey, setShowApiKey] = useState(false);
  const [apiKeyDirty, setApiKeyDirty] = useState(false);
  const [templateCopied, setTemplateCopied] = useState(false);

  useEffect(() => {
    setFormData(buildInitialForm(provider));
    setFieldError('');
    setShowApiKey(false);
    setApiKeyDirty(false);
    setTemplateCopied(false);
  }, [provider, mode]);

  const updateField = (key, value) => {
    if (key === 'apiKey') {
      setApiKeyDirty(true);
    }
    setFormData((prev) => ({ ...prev, [key]: value }));
    if (fieldError) {
      setFieldError('');
    }
  };

  const handleSubmit = async (event) => {
    event.preventDefault();

    const payload = {
      name: formData.name.trim(),
      providerType: formData.providerType.trim(),
      baseURL: formData.baseURL.trim(),
      apiKey: formData.apiKey.trim(),
      model: formData.model.trim(),
      reasoningEffort: formData.reasoningEffort.trim() || 'xhigh',
      enable1MContext: Boolean(formData.enable1MContext),
      website: formData.website.trim()
    };

    if (!payload.name || !payload.providerType || !payload.baseURL || !payload.apiKey || !payload.model || !payload.reasoningEffort) {
      setFieldError('请填写提供商标识、接入类型、Base URL、API Key、默认模型和推理强度');
      return;
    }

    await onSave?.(payload);
  };

  const isEdit = mode === 'edit';
  const isMaskedApiKeyPreview = isEdit && !showApiKey && !apiKeyDirty && !!provider?.apiKeyMasked;
  const apiKeyInputType = isMaskedApiKeyPreview ? 'text' : (showApiKey ? 'text' : 'password');
  const apiKeyInputValue = isMaskedApiKeyPreview ? provider?.apiKeyMasked || '' : formData.apiKey;
  const templateProviderName = formData.name.trim() || 'xxx';
  const templateModel = formData.model.trim() || 'gpt-5.4';
  const templateReasoning = formData.reasoningEffort.trim() || 'xhigh';
  const templateBaseURL = formData.baseURL.trim() || 'https://.....';
  const templatePreview = [
    `model = "${templateModel}"`,
    ...(formData.enable1MContext ? [`model_context_window = ${MODEL_CONTEXT_WINDOW_1M}`] : []),
    `model_reasoning_effort = "${templateReasoning}"`,
    `model_provider = "${templateProviderName}"`,
    'network_access = "enabled"',
    'disable_response_storage = true',
    'windows_wsl_setup_acknowledged = true',
    'model_verbosity = "high"',
    '',
    `[model_providers.${templateProviderName}]`,
    `name = "${templateProviderName}"`,
    `base_url = "${templateBaseURL}"`,
    'wire_api = "responses"',
    'requires_openai_auth = true'
  ].join('\n');
  const templateLines = templatePreview.split('\n');

  const handleCopyTemplate = async () => {
    if (!globalThis?.navigator?.clipboard?.writeText) {
      setFieldError('当前环境不支持复制到剪贴板');
      return;
    }

    try {
      await globalThis.navigator.clipboard.writeText(templatePreview);
      setTemplateCopied(true);
      setTimeout(() => setTemplateCopied(false), 1600);
    } catch (error) {
      setFieldError(getErrorMessage(error, '复制模板失败'));
    }
  };

  return (
    <div className="fixed inset-0 z-[60] flex items-center justify-center bg-slate-900/30 p-4 backdrop-blur-sm animate-fade-in">
      <div className="flex max-h-[calc(100vh-2rem)] w-full max-w-3xl flex-col overflow-hidden rounded-2xl bg-white shadow-xl">
        <div className="flex items-start justify-between gap-4 border-b border-slate-100 px-6 py-4">
          <div className="flex items-start gap-3">
            <div className="rounded-xl bg-slate-900 p-2 text-white shadow-sm">
              {isEdit ? <Pencil size={18} /> : <Plus size={18} />}
            </div>
            <div>
              <h2 className="text-lg font-semibold text-slate-900">{isEdit ? '编辑提供商' : '添加提供商'}</h2>
            </div>
          </div>
          <button
            type="button"
            onClick={onCancel}
            className="rounded-lg p-2 text-slate-400 transition-colors hover:bg-slate-100 hover:text-slate-700"
            disabled={loading}
            title="关闭"
          >
            <XCircle size={18} />
          </button>
        </div>

        <form className="space-y-4 overflow-y-auto px-6 py-6" onSubmit={handleSubmit} autoComplete="off">
          {(fieldError || serverError) && (
            <div className="rounded-lg border border-rose-200 bg-rose-50 px-4 py-3 text-sm text-rose-700">
              {fieldError || serverError}
            </div>
          )}

          <div className="grid grid-cols-1 gap-6 lg:grid-cols-[minmax(0,1fr)_320px]">
            <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
            <div>
              <Input
                label="提供商标识"
                value={formData.name}
                onChange={(event) => updateField('name', event.target.value)}
                placeholder="例如：custom"
                autoFocus
              />
            </div>

            <FormSelectField
              label="接入类型"
              value={formData.providerType}
              onChange={(value) => updateField('providerType', value)}
              options={PROVIDER_TYPE_OPTIONS}
            />

            <Input
              label="官网地址（可选）"
              value={formData.website}
              onChange={(event) => updateField('website', event.target.value)}
              placeholder="https://platform.openai.com"
            />

            <Input
              label="默认模型"
              value={formData.model}
              onChange={(event) => updateField('model', event.target.value)}
              placeholder="例如：gpt-5.4"
            />

            <div className="sm:col-span-2">
              <ReasoningEffortSelector
                value={formData.reasoningEffort}
                onChange={(value) => updateField('reasoningEffort', value)}
              />
            </div>

            <FormToggleField
              label="1M 上下文窗口"
              checked={formData.enable1MContext}
              onChange={(value) => updateField('enable1MContext', value)}
              help="开启后，当前 provider 激活时会向 config.toml 额外写入 model_context_window = 1000000。"
            />

            <div className="sm:col-span-2">
              <Input
                label="Base URL"
                value={formData.baseURL}
                onChange={(event) => updateField('baseURL', event.target.value)}
                placeholder="https://code.newcli.com/codex/v1"
              />
            </div>

            <div className="sm:col-span-2">
              <label className="mb-1.5 block text-sm font-medium text-slate-700">API Key</label>
              <div className="relative">
                <input
                  type={apiKeyInputType}
                  value={apiKeyInputValue}
                  onChange={(event) => updateField('apiKey', event.target.value)}
                  placeholder="sk-..."
                  autoComplete="new-password"
                  data-form-type="other"
                  readOnly={isMaskedApiKeyPreview}
                  className="w-full rounded-lg border border-slate-200 px-3 py-2 pr-11 text-sm focus:border-indigo-500 focus:outline-none focus:ring-2 focus:ring-indigo-500 [&::-ms-reveal]:hidden [&::-ms-clear]:hidden [&::-webkit-credentials-auto-fill-button]:hidden"
                />
                <button
                  type="button"
                  onClick={() => setShowApiKey((prev) => !prev)}
                  className="absolute right-2 top-1/2 -translate-y-1/2 rounded-md p-1 text-slate-400 transition-colors hover:text-slate-600"
                  title={showApiKey ? '隐藏真实 API Key' : '显示真实 API Key'}
                >
                  {showApiKey ? <EyeOff size={18} /> : <Eye size={18} />}
                </button>
              </div>
            </div>
            </div>

            <div className="rounded-2xl border border-slate-200 bg-slate-950 text-slate-100 shadow-inner">
              <div className="relative border-b border-white/10 px-4 py-3 pr-28">
                <div>
                  <div className="text-sm font-semibold">Service Config Template</div>
                  <div className="text-[11px] uppercase tracking-[0.16em] text-slate-400">Live Preview</div>
                </div>
                <div className="absolute right-4 top-3 flex items-center gap-2">
                  <span className="rounded-full border border-emerald-400/20 bg-emerald-400/10 px-2.5 py-1 text-[10px] font-semibold uppercase tracking-[0.14em] text-emerald-300">
                    TOML
                  </span>
                  <button
                    type="button"
                    onClick={handleCopyTemplate}
                    className="inline-flex h-7 w-7 items-center justify-center rounded-full border border-white/10 bg-white/5 text-slate-200 transition-colors hover:bg-white/10"
                    title={templateCopied ? '已复制模板' : '复制完整模板'}
                    aria-label={templateCopied ? '已复制模板' : '复制完整模板'}
                  >
                    {templateCopied ? <CheckCircle2 size={14} /> : <Copy size={14} />}
                  </button>
                </div>
              </div>
              <div className="px-4 py-4">
                <div className="space-y-0.5 font-mono text-[12px] leading-6 text-slate-200">
                  {templateLines.map((line, index) => (
                    line ? (
                      <div
                        key={`${index}-${line}`}
                        className="truncate whitespace-nowrap"
                        title={line}
                      >
                        {line}
                      </div>
                    ) : (
                      <div key={`blank-${index}`} className="h-6" />
                    )
                  ))}
                </div>
              </div>
            </div>
          </div>

          <div className="flex justify-end gap-3 border-t border-slate-100 pt-5">
            <Button type="button" variant="ghost" onClick={onCancel} disabled={loading}>
              取消
            </Button>
            <Button type="submit" icon={isEdit ? Pencil : Plus} loading={loading}>
              {isEdit ? '保存修改' : '添加提供商'}
            </Button>
          </div>
        </form>
      </div>
    </div>
  );
};

const DeleteProviderConfirmDialog = ({ provider, loading, onCancel, onConfirm }) => {
  if (!provider) return null;

  return (
    <div className="fixed inset-0 z-[60] flex items-center justify-center bg-slate-900/30 p-4 backdrop-blur-sm animate-fade-in">
      <div className="w-full max-w-md rounded-2xl bg-white p-6 shadow-xl">
        <div className="mb-4 flex items-start gap-3">
          <div className="rounded-full bg-rose-100 p-3">
            <TriangleAlert className="text-rose-600" size={22} />
          </div>
          <div>
            <h3 className="text-lg font-semibold text-slate-900">确认删除提供商</h3>
            <p className="mt-1 text-sm text-slate-500">不会处理渠道、计费或请求追踪数据</p>
          </div>
        </div>

        <p className="mb-5 text-sm leading-6 text-slate-600">
          确定要删除 <span className="font-semibold text-slate-900">“{provider.name}”</span> 吗？
          如果它是当前接管项，系统会尝试切换到其他已保存的提供商。
        </p>

        <div className="flex justify-end gap-3">
          <Button variant="ghost" onClick={onCancel} disabled={loading}>
            取消
          </Button>
          <Button variant="danger" icon={Trash2} loading={loading} onClick={onConfirm}>
            删除提供商
          </Button>
        </div>
      </div>
    </div>
  );
};

const ProviderCard = ({ provider, actionLoading, onActivate, onEdit, onDelete }) => {
  const isSwitching = actionLoading === `activate-${provider.id}`;
  const isDeleting = actionLoading === `delete-${provider.id}`;

  return (
    <div
      className={`rounded-xl border bg-white p-4 shadow-sm transition-shadow hover:shadow-md ${
        provider.isActive ? 'border-emerald-200 shadow-emerald-100/40' : 'border-slate-200/70'
      }`}
    >
      <div className="flex items-start justify-between gap-4">
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-2">
            <h3 className="text-base font-semibold text-slate-900">{provider.name}</h3>
            <span
              className={`inline-flex items-center rounded-full border px-2.5 py-1 text-xs font-medium ${
                provider.isActive
                  ? 'border-emerald-200 bg-emerald-50 text-emerald-700'
                  : 'border-slate-200 bg-slate-50 text-slate-500'
              }`}
            >
              {provider.isActive ? '当前接管' : '可切换'}
            </span>
          </div>

          <div className="mt-2 text-xs text-slate-500">
            写入 model_provider = <span className="font-mono text-slate-700">{provider.name}</span>，并同步对应 provider 的模型参数与 API Key
          </div>

          <div className="mt-3 flex flex-wrap items-center gap-2 text-xs">
            <span className="inline-flex items-center rounded-full border border-indigo-200 bg-indigo-50 px-2.5 py-1 font-medium text-indigo-700">
              <Server size={12} className="mr-1.5" />
              {formatProviderType(provider.providerType)}
            </span>
            <span className="inline-flex items-center rounded-full border border-emerald-200 bg-emerald-50 px-2.5 py-1 font-medium text-emerald-700">
              <ShieldCheck size={12} className="mr-1.5" />
              {provider.model}
            </span>
            <span className="inline-flex items-center rounded-full border border-amber-200 bg-amber-50 px-2.5 py-1 font-medium text-amber-700">
              推理强度 {provider.reasoningEffort || 'xhigh'}
            </span>
            {provider.enable1MContext && (
              <span className="inline-flex items-center rounded-full border border-sky-200 bg-sky-50 px-2.5 py-1 font-medium text-sky-700">
                1M 上下文
              </span>
            )}
          </div>
        </div>

        {provider.website && (
          <button
            type="button"
            className="inline-flex items-center rounded-lg border border-slate-200 px-3 py-2 text-xs font-medium text-slate-500 transition-colors hover:border-slate-300 hover:text-slate-900"
            onClick={() => openExternalURL(provider.website)}
          >
            <Globe size={14} className="mr-1.5" />
            官网
          </button>
        )}
      </div>

      <div className="mt-4 grid grid-cols-1 gap-3 sm:grid-cols-2">
        <InfoTile title="Base URL" value={provider.baseURL} icon={Globe} mono titleAttr={provider.baseURL} />
        <InfoTile title="API Key" value={provider.apiKeyMasked || '未显示'} icon={KeyRound} mono />
        <InfoTile title="默认模型" value={provider.model} icon={ShieldCheck} />
        <InfoTile title="推理强度" value={provider.reasoningEffort || 'xhigh'} icon={ArrowRightLeft} />
      </div>

      <div className="mt-4 flex flex-wrap items-center justify-between gap-3 border-t border-slate-100 pt-4">
        <div className="text-xs text-slate-400">
          更新时间 {formatDateTime(provider.updatedAt || provider.createdAt)}
        </div>

        <div className="flex flex-wrap items-center gap-2">
          <Button
            variant={provider.isActive ? 'secondary' : 'primary'}
            size="sm"
            icon={provider.isActive ? CheckCircle2 : ArrowRightLeft}
            onClick={() => onActivate?.(provider)}
            loading={isSwitching}
            disabled={provider.isActive || isDeleting}
          >
            {provider.isActive ? '当前使用中' : '设为当前'}
          </Button>
          <Button
            variant="ghost"
            size="sm"
            icon={Pencil}
            onClick={() => onEdit?.(provider)}
            disabled={isSwitching || isDeleting}
          >
            编辑
          </Button>
          <Button
            variant="dangerSoft"
            size="sm"
            icon={Trash2}
            onClick={() => onDelete?.(provider)}
            loading={isDeleting}
            disabled={isSwitching}
          >
            删除
          </Button>
        </div>
      </div>
    </div>
  );
};

const CodexPage = () => {
  const [providers, setProviders] = useState([]);
  const [storageStatus, setStorageStatus] = useState(null);
  const [loading, setLoading] = useState(true);
  const [actionLoading, setActionLoading] = useState('');
  const [error, setError] = useState('');
  const [showForm, setShowForm] = useState(false);
  const [formMode, setFormMode] = useState('create');
  const [editingProvider, setEditingProvider] = useState(null);
  const [deleteTarget, setDeleteTarget] = useState(null);
  const [formError, setFormError] = useState('');
  const [lastUpdate, setLastUpdate] = useState('');

  const activeProvider = providers.find((provider) => provider.isActive) || null;
  const overviewCards = [
    {
      title: '当前接管',
      value: activeProvider?.name || '未设置',
      tooltip: activeProvider?.name || '未设置',
      subText: activeProvider ? '已接管 config.toml / auth.json' : '请选择一个提供商设为当前',
      icon: CheckCircle2,
      statusColor: 'bg-emerald-50 text-emerald-600'
    },
    {
      title: '当前模型',
      value: activeProvider?.model || '等待接管',
      tooltip: activeProvider?.model || '等待接管',
      subText: activeProvider
        ? `推理强度 ${activeProvider.reasoningEffort || 'xhigh'}${activeProvider.enable1MContext ? ' · 1M 上下文' : ''}`
        : '接管后自动同步模型参数',
      icon: ShieldCheck,
      statusColor: 'bg-sky-50 text-sky-600'
    },
    {
      title: '已保存提供商',
      value: String(storageStatus?.totalCount || providers.length),
      tooltip: String(storageStatus?.totalCount || providers.length),
      subText: '数据库中维护全部 provider 配置',
      icon: Database,
      statusColor: 'bg-slate-100 text-slate-600'
    },
    {
      title: '存储状态',
      value: storageStatus?.enabled ? 'SQLite' : 'Pending',
      tooltip: storageStatus?.enabled ? 'SQLite' : 'Pending',
      subText: lastUpdate ? `更新于 ${lastUpdate}` : '等待首次同步',
      icon: Server,
      statusColor: 'bg-amber-50 text-amber-600'
    }
  ];

  const loadData = useCallback(async ({ silent = false } = {}) => {
    if (!silent) {
      setLoading(true);
    }

    try {
      const [status, providerList] = await Promise.all([
        getCodexProviderStorageStatus(),
        getCodexProviders()
      ]);

      setStorageStatus(status);
      setProviders(providerList);
      setError('');
      setLastUpdate(formatDateTime(new Date().toISOString()));
    } catch (loadError) {
      setError(getErrorMessage(loadError, '加载 Codex 提供商失败'));
    } finally {
      if (!silent) {
        setLoading(false);
      }
    }
  }, []);

  useEffect(() => {
    if (!isWailsEnvironment()) {
      setError('当前环境未启用 Wails，无法管理 Codex 提供商');
      setLoading(false);
      return undefined;
    }

    loadData();

    const unsubscribe = subscribeToEvent('codex-provider:update', () => {
      loadData({ silent: true });
    });

    return () => {
      unsubscribe?.();
    };
  }, [loadData]);

  const handleRefresh = async () => {
    try {
      setActionLoading('refresh');
      await loadData();
    } finally {
      setActionLoading('');
    }
  };

  const handleCreate = () => {
    setFormMode('create');
    setEditingProvider(null);
    setFormError('');
    setShowForm(true);
  };

  const handleEdit = (provider) => {
    setFormMode('edit');
    setEditingProvider(provider);
    setFormError('');
    setShowForm(true);
  };

  const handleSave = async (payload) => {
    const loadingKey = formMode === 'create' ? 'create' : `save-${editingProvider?.id || 0}`;

    try {
      setActionLoading(loadingKey);
      setFormError('');

      if (formMode === 'create') {
        await createCodexProvider(payload);
      } else if (editingProvider) {
        await updateCodexProviderByID(editingProvider.id, payload);
      }

      setShowForm(false);
      setEditingProvider(null);
      await loadData({ silent: true });
    } catch (saveError) {
      setFormError(getErrorMessage(saveError, '保存提供商失败'));
    } finally {
      setActionLoading('');
    }
  };

  const handleActivate = async (provider) => {
    try {
      setActionLoading(`activate-${provider.id}`);
      await activateCodexProvider(provider.id);
      await loadData({ silent: true });
    } catch (activateError) {
      setError(getErrorMessage(activateError, '切换提供商失败'));
    } finally {
      setActionLoading('');
    }
  };

  const handleConfirmDelete = async () => {
    if (!deleteTarget) return;

    try {
      setActionLoading(`delete-${deleteTarget.id}`);
      await deleteCodexProviderByID(deleteTarget.id);
      setDeleteTarget(null);
      await loadData({ silent: true });
    } catch (deleteError) {
      setError(getErrorMessage(deleteError, '删除提供商失败'));
    } finally {
      setActionLoading('');
    }
  };

  if (loading && !storageStatus) {
    return <LoadingSpinner text="加载 Codex 提供商..." />;
  }

  if (error && !storageStatus && providers.length === 0) {
    return (
      <ErrorMessage
        title="Codex 提供商加载失败"
        message={error}
        onRetry={handleRefresh}
      />
    );
  }

  return (
    <div className="mx-auto max-w-7xl space-y-5 animate-fade-in">
      <div className="flex flex-wrap items-end justify-between gap-4">
        <div className="flex items-center gap-3">
          <div className="rounded-xl bg-slate-900 p-2 text-white shadow-sm">
            <Database className="h-5 w-5" />
          </div>
          <div>
            <h1 className="text-2xl font-bold text-slate-900">Codex 管理</h1>
            <p className="mt-1 text-sm text-slate-500">
              所有 provider 先存数据库；只有当前激活项会同步到 config.toml 与 auth.json
            </p>
          </div>
        </div>

        <div className="flex items-center gap-3">
          <Button
            variant="ghost"
            size="sm"
            icon={RefreshCw}
            onClick={handleRefresh}
            loading={actionLoading === 'refresh'}
          >
            刷新
          </Button>
          <Button size="sm" icon={Plus} onClick={handleCreate}>
            添加提供商
          </Button>
        </div>
      </div>

      <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 xl:grid-cols-4">
        {overviewCards.map((card) => (
          <KPICard
            key={card.title}
            title={card.title}
            value={card.value}
            tooltip={card.tooltip}
            subText={card.subText}
            icon={card.icon}
            statusColor={card.statusColor}
            layout="side"
          />
        ))}
      </div>

      {error && (
        <div className="flex items-start gap-3 rounded-xl border border-rose-200 bg-rose-50 px-4 py-3 text-sm text-rose-700">
          <TriangleAlert className="mt-0.5 h-4 w-4 flex-shrink-0" />
          <div>{error}</div>
        </div>
      )}

      {providers.length > 0 && !storageStatus?.hasActive && (
        <div className="flex items-start gap-3 rounded-xl border border-amber-200 bg-amber-50 px-4 py-3 text-sm text-amber-700">
          <TriangleAlert className="mt-0.5 h-4 w-4 flex-shrink-0" />
          <div>当前没有正在接管的提供商，请从列表中选择一个设为当前。</div>
        </div>
      )}

      {providers.length === 0 ? (
        <div className="rounded-xl border border-slate-200/70 bg-white shadow-sm">
          <EmptyState
            icon={Database}
            title="暂无提供商"
            description="先添加一个提供商；系统会把所有 provider 存到数据库中，切换时只把当前项的完整配置同步到 Codex。"
            action={
              <Button size="sm" icon={Plus} onClick={handleCreate}>
                添加提供商
              </Button>
            }
          />
        </div>
      ) : (
        <div className="grid grid-cols-1 gap-4 xl:grid-cols-2">
          {providers.map((provider) => (
            <ProviderCard
              key={provider.id}
              provider={provider}
              actionLoading={actionLoading}
              onActivate={handleActivate}
              onEdit={handleEdit}
              onDelete={setDeleteTarget}
            />
          ))}
        </div>
      )}

      {showForm && (
        <ProviderFormModal
          mode={formMode}
          provider={editingProvider}
          loading={actionLoading === 'create' || actionLoading === `save-${editingProvider?.id || 0}`}
          serverError={formError}
          onCancel={() => {
            setShowForm(false);
            setEditingProvider(null);
            setFormError('');
          }}
          onSave={handleSave}
        />
      )}

      {deleteTarget && (
        <DeleteProviderConfirmDialog
          provider={deleteTarget}
          loading={actionLoading === `delete-${deleteTarget.id}`}
          onCancel={() => setDeleteTarget(null)}
          onConfirm={handleConfirmDelete}
        />
      )}
    </div>
  );
};

export default CodexPage;

