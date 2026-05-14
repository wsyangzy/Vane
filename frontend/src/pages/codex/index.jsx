// ============================================
// Codex 页面 - 渠道-端点 两层管理
// 2026-05-13: 引入「渠道（共用配置）+ 端点（仅 base_url）」两层模型
// 切换激活端点时，仅 ~/.codex/config.toml 中所属渠道 section 的 base_url 行变化
// ============================================

import { useCallback, useEffect, useRef, useState } from 'react';
import {
  ArrowRightLeft,
  CheckCircle2,
  ChevronDown,
  Copy,
  Database,
  Eye,
  EyeOff,
  Globe,
  KeyRound,
  Loader2,
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
  EmptyState,
  ErrorMessage,
  Input,
  KPICard,
  LoadingSpinner
} from '@components/ui';
import { renderModalPortal } from '@components/ui/modalPortal.js';
import {
  activateCodexChannel,
  activateCodexEndpoint,
  createCodexChannel,
  createCodexEndpoint,
  deleteCodexChannelByID,
  deleteCodexEndpointByID,
  getCodexChannels,
  getCodexProviderStorageStatus,
  isWailsEnvironment,
  openExternalURL,
  subscribeToEvent,
  updateCodexChannelByID,
  updateCodexEndpointByID
} from '@utils/wailsApi.js';

const REASONING_EFFORT_OPTIONS = [
  { value: 'low', label: 'low', hint: 'Fastest' },
  { value: 'medium', label: 'medium', hint: 'Balanced' },
  { value: 'high', label: 'high', hint: 'Deeper' },
  { value: 'xhigh', label: 'xhigh', hint: 'Max' }
];
const MODEL_CONTEXT_WINDOW_1M = 1000000;

// Codex 官方品牌图标（来源：lobe-icons / OpenAI Codex）
// 使用 currentColor 填充，可被父级 text-white 等颜色 token 控制
const CodexLogo = ({ className = 'w-6 h-6' }) => (
  <svg
    className={className}
    viewBox="0 0 24 24"
    fill="currentColor"
    fillRule="evenodd"
    aria-hidden="true"
    focusable="false"
  >
    <title>Codex</title>
    <path
      clipRule="evenodd"
      d="M8.086.457a6.105 6.105 0 013.046-.415c1.333.153 2.521.72 3.564 1.7a.117.117 0 00.107.029c1.408-.346 2.762-.224 4.061.366l.063.03.154.076c1.357.703 2.33 1.77 2.918 3.198.278.679.418 1.388.421 2.126a5.655 5.655 0 01-.18 1.631.167.167 0 00.04.155 5.982 5.982 0 011.578 2.891c.385 1.901-.01 3.615-1.183 5.14l-.182.22a6.063 6.063 0 01-2.934 1.851.162.162 0 00-.108.102c-.255.736-.511 1.364-.987 1.992-1.199 1.582-2.962 2.462-4.948 2.451-1.583-.008-2.986-.587-4.21-1.736a.145.145 0 00-.14-.032c-.518.167-1.04.191-1.604.185a5.924 5.924 0 01-2.595-.622 6.058 6.058 0 01-2.146-1.781c-.203-.269-.404-.522-.551-.821a7.74 7.74 0 01-.495-1.283 6.11 6.11 0 01-.017-3.064.166.166 0 00.008-.074.115.115 0 00-.037-.064 5.958 5.958 0 01-1.38-2.202 5.196 5.196 0 01-.333-1.589 6.915 6.915 0 01.188-2.132c.45-1.484 1.309-2.648 2.577-3.493.282-.188.55-.334.802-.438.286-.12.573-.22.861-.304a.129.129 0 00.087-.087A6.016 6.016 0 015.635 2.31C6.315 1.464 7.132.846 8.086.457zm-.804 7.85a.848.848 0 00-1.473.842l1.694 2.965-1.688 2.848a.849.849 0 001.46.864l1.94-3.272a.849.849 0 00.007-.854l-1.94-3.393zm5.446 6.24a.849.849 0 000 1.695h4.848a.849.849 0 000-1.696h-4.848z"
    />
  </svg>
);

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

const formatDateTime = (value) => {
  if (!value) return '-';
  return value;
};

const buildInitialChannelForm = (channel) => ({
  name: channel?.name || '',
  providerType: channel?.providerType || 'openai-compatible',
  apiKey: channel?.apiKey || '',
  model: channel?.model || '',
  reasoningEffort: channel?.reasoningEffort || 'xhigh',
  enable1MContext: Boolean(channel?.enable1MContext),
  website: channel?.website || ''
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

// ============================================================================
// 渠道编辑弹窗（不含 base_url；端点单独管理）
// ============================================================================
const ChannelFormModal = ({ mode, channel, loading, serverError, onCancel, onSave }) => {
  const [formData, setFormData] = useState(() => buildInitialChannelForm(channel));
  const [fieldError, setFieldError] = useState('');
  const [showApiKey, setShowApiKey] = useState(false);
  const [apiKeyDirty, setApiKeyDirty] = useState(false);
  const [templateCopied, setTemplateCopied] = useState(false);
  const isWails = isWailsEnvironment();
  const modalTopPadding = isWails ? 48 : 16;
  const modalBottomPadding = 16;
  const modalMaxHeight = `calc(100vh - ${modalTopPadding + modalBottomPadding}px)`;

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
      providerType: formData.providerType.trim() || 'openai-compatible',
      apiKey: formData.apiKey.trim(),
      model: formData.model.trim(),
      reasoningEffort: formData.reasoningEffort.trim() || 'xhigh',
      enable1MContext: Boolean(formData.enable1MContext),
      website: formData.website.trim()
    };

    if (!payload.name || !payload.apiKey || !payload.model || !payload.reasoningEffort) {
      setFieldError('请填写渠道名、API Key、默认模型和推理强度');
      return;
    }

    await onSave?.(payload);
  };

  const isEdit = mode === 'edit';
  const isMaskedApiKeyPreview = isEdit && !showApiKey && !apiKeyDirty && !!channel?.apiKeyMasked;
  const apiKeyInputType = isMaskedApiKeyPreview ? 'text' : (showApiKey ? 'text' : 'password');
  const apiKeyInputValue = isMaskedApiKeyPreview ? channel?.apiKeyMasked || '' : formData.apiKey;
  const templateChannelName = formData.name.trim() || 'xxx';
  const templateModel = formData.model.trim() || 'gpt-5.4';
  const templateReasoning = formData.reasoningEffort.trim() || 'xhigh';
  const templatePreview = [
    `model = "${templateModel}"`,
    ...(formData.enable1MContext ? [`model_context_window = ${MODEL_CONTEXT_WINDOW_1M}`] : []),
    `model_reasoning_effort = "${templateReasoning}"`,
    `model_provider = "${templateChannelName}"`,
    'network_access = "enabled"',
    'disable_response_storage = true',
    'windows_wsl_setup_acknowledged = true',
    'model_verbosity = "high"',
    '',
    `[model_providers.${templateChannelName}]`,
    `name = "${templateChannelName}"`,
    'base_url = "<由激活端点动态写入>"',
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

  return renderModalPortal(
    <div
      className="fixed inset-0 z-[100] flex items-start justify-center overflow-y-auto bg-slate-900/30 px-4 pb-4 backdrop-blur-sm animate-fade-in"
      style={{ paddingTop: `${modalTopPadding}px` }}
    >
      <div
        className="flex w-full max-w-3xl flex-col overflow-hidden rounded-2xl bg-white shadow-xl"
        style={{ maxHeight: modalMaxHeight }}
      >
        <div className="flex items-start justify-between gap-4 border-b border-slate-100 px-6 py-4">
          <div className="flex items-start gap-3">
            <div className="rounded-xl bg-slate-900 p-2 text-white shadow-sm">
              {isEdit ? <Pencil size={18} /> : <Plus size={18} />}
            </div>
            <div>
              <h2 className="text-lg font-semibold text-slate-900">{isEdit ? '编辑渠道' : '添加渠道'}</h2>
              <p className="mt-0.5 text-xs text-slate-500">渠道承载共用配置；具体 base_url 在端点中单独管理</p>
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

        <form className="flex min-h-0 flex-1 flex-col overflow-hidden" onSubmit={handleSubmit} autoComplete="off">
          <div className="space-y-4 overflow-y-auto px-6 py-6">
            {(fieldError || serverError) && (
              <div className="rounded-lg border border-rose-200 bg-rose-50 px-4 py-3 text-sm text-rose-700">
                {fieldError || serverError}
              </div>
            )}

            <div className="grid grid-cols-1 gap-6 lg:grid-cols-[minmax(0,1fr)_320px]">
              <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
                <div>
                  <Input
                    label="渠道名"
                    value={formData.name}
                    onChange={(event) => updateField('name', event.target.value)}
                    placeholder="例如：custom"
                    autoFocus
                  />
                </div>

                <Input
                  label="默认模型"
                  value={formData.model}
                  onChange={(event) => updateField('model', event.target.value)}
                  placeholder="例如：gpt-5.4"
                />

                <div className="sm:col-span-2">
                  <Input
                    label="官网地址（可选）"
                    value={formData.website}
                    onChange={(event) => updateField('website', event.target.value)}
                    placeholder="https://platform.openai.com"
                  />
                </div>

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
                  help="开启后，当前渠道激活时会向 config.toml 额外写入 model_context_window = 1000000。"
                />

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
                  {isMaskedApiKeyPreview && (
                    <p className="mt-1 text-xs text-slate-400">点击图标查看真实值或直接输入新 Key 替换</p>
                  )}
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
          </div>

          <div className="flex flex-shrink-0 items-center justify-end gap-2 border-t border-slate-100 bg-slate-50/40 px-6 py-3">
            <Button variant="secondary" onClick={onCancel} disabled={loading}>
              取消
            </Button>
            <Button variant="primary" icon={isEdit ? Pencil : Plus} loading={loading} type="submit">
              {isEdit ? '保存修改' : '创建渠道'}
            </Button>
          </div>
        </form>
      </div>
    </div>
  );
};

// ============================================================================
// 端点编辑弹窗（name + base_url）
// ============================================================================
const EndpointFormModal = ({ mode, channel, endpoint, loading, serverError, onCancel, onSave }) => {
  const [name, setName] = useState(endpoint?.name || '');
  const [baseURL, setBaseURL] = useState(endpoint?.baseURL || '');
  const [fieldError, setFieldError] = useState('');
  const isWails = isWailsEnvironment();
  const modalTopPadding = isWails ? 48 : 16;

  const handleSubmit = async (event) => {
    event.preventDefault();
    const trimmedName = name.trim();
    const trimmed = baseURL.trim();
    if (!trimmedName) {
      setFieldError('端点名称不能为空');
      return;
    }
    if (!trimmed) {
      setFieldError('base_url 不能为空');
      return;
    }
    if (!/^https?:\/\//i.test(trimmed)) {
      setFieldError('base_url 必须以 http:// 或 https:// 开头');
      return;
    }
    await onSave?.({ name: trimmedName, baseURL: trimmed });
  };

  const isEdit = mode === 'edit';

  return renderModalPortal(
    <div
      className="fixed inset-0 z-[100] flex items-start justify-center overflow-y-auto bg-slate-900/30 px-4 pb-4 backdrop-blur-sm animate-fade-in"
      style={{ paddingTop: `${modalTopPadding}px` }}
    >
      <div className="flex w-full max-w-md flex-col overflow-hidden rounded-2xl bg-white shadow-xl">
        <div className="flex items-start justify-between gap-4 border-b border-slate-100 px-6 py-4">
          <div className="flex items-start gap-3">
            <div className="rounded-xl bg-slate-900 p-2 text-white shadow-sm">
              {isEdit ? <Pencil size={18} /> : <Plus size={18} />}
            </div>
            <div>
              <h2 className="text-lg font-semibold text-slate-900">{isEdit ? '编辑端点' : '添加端点'}</h2>
              <p className="mt-0.5 text-xs text-slate-500">渠道：{channel?.name}</p>
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

        <form onSubmit={handleSubmit} autoComplete="off">
          <div className="space-y-4 px-6 py-6">
            {(fieldError || serverError) && (
              <div className="rounded-lg border border-rose-200 bg-rose-50 px-4 py-3 text-sm text-rose-700">
                {fieldError || serverError}
              </div>
            )}

            <div>
              <label className="mb-1.5 block text-sm font-medium text-slate-700">端点名称</label>
              <input
                type="text"
                autoFocus
                value={name}
                onChange={(event) => {
                  setName(event.target.value);
                  if (fieldError) setFieldError('');
                }}
                placeholder="如：主线路 / 备用 / 上海节点"
                className="w-full rounded-lg border border-slate-200 px-3 py-2 text-sm focus:border-indigo-500 focus:outline-none focus:ring-2 focus:ring-indigo-500"
              />
              <p className="mt-1 text-xs text-slate-400">同一渠道下端点名称唯一，仅作 UI 标识。</p>
            </div>

            <div>
              <label className="mb-1.5 block text-sm font-medium text-slate-700">Base URL</label>
              <input
                type="text"
                value={baseURL}
                onChange={(event) => {
                  setBaseURL(event.target.value);
                  if (fieldError) setFieldError('');
                }}
                placeholder="https://code.newcli.com/codex/v1"
                className="w-full rounded-lg border border-slate-200 px-3 py-2 font-mono text-sm focus:border-indigo-500 focus:outline-none focus:ring-2 focus:ring-indigo-500"
              />
              <p className="mt-1 text-xs text-slate-400">
                切换激活端点时仅 config.toml 中该渠道 section 的 base_url 行会变化。
              </p>
            </div>
          </div>

          <div className="flex flex-shrink-0 items-center justify-end gap-2 border-t border-slate-100 bg-slate-50/40 px-6 py-3">
            <Button variant="secondary" onClick={onCancel} disabled={loading}>
              取消
            </Button>
            <Button variant="primary" icon={isEdit ? Pencil : Plus} loading={loading} type="submit">
              {isEdit ? '保存修改' : '添加端点'}
            </Button>
          </div>
        </form>
      </div>
    </div>
  );
};

// ============================================================================
// 删除确认弹窗（渠道 / 端点 共用）
// ============================================================================
const DeleteConfirmDialog = ({ title, description, hint, loading, onCancel, onConfirm }) => {
  const isWails = isWailsEnvironment();
  const modalTopPadding = isWails ? 48 : 16;

  return renderModalPortal(
    <div
      className="fixed inset-0 z-[100] flex items-start justify-center overflow-y-auto bg-slate-900/30 px-4 pb-4 backdrop-blur-sm animate-fade-in"
      style={{ paddingTop: `${modalTopPadding}px` }}
    >
      <div className="flex w-full max-w-md flex-col overflow-hidden rounded-2xl bg-white shadow-xl">
        <div className="flex items-start gap-3 border-b border-slate-100 px-6 py-4">
          <div className="rounded-xl bg-rose-100 p-2 text-rose-600 shadow-sm">
            <TriangleAlert size={18} />
          </div>
          <div>
            <h2 className="text-lg font-semibold text-slate-900">{title}</h2>
            {description && <p className="mt-1 text-sm text-slate-500">{description}</p>}
          </div>
        </div>
        {hint && <div className="px-6 py-4 text-sm text-slate-600">{hint}</div>}
        <div className="flex items-center justify-end gap-2 border-t border-slate-100 bg-slate-50/40 px-6 py-3">
          <Button variant="secondary" onClick={onCancel} disabled={loading}>
            取消
          </Button>
          <Button variant="danger" icon={Trash2} loading={loading} onClick={onConfirm}>
            确认删除
          </Button>
        </div>
      </div>
    </div>
  );
};

// ============================================================================
// 渠道卡片（卡片头 + 端点子列表）
// ============================================================================
// ============================================================================
// 端点行（紧凑表格风，参考 channels/EndpointRow）：
// 左侧 toggle = 激活；行整体点击展开内嵌编辑（base_url 直改即保存）
// ============================================================================
const summarizeEndpoint = (baseURL) => {
  try {
    const u = new URL(baseURL);
    return u.host + (u.pathname && u.pathname !== '/' ? u.pathname : '');
  } catch {
    return baseURL || '';
  }
};

// ============================================================================
// 端点行（精简表格风）：
//   左侧 toggle = 激活；右侧 hover 浮现编辑/删除按钮（编辑走弹窗，不再使用行内展开）
// ============================================================================
const EndpointRow = ({
  endpoint,
  onActivate,
  onEdit,
  onDelete,
  actionLoading
}) => {
  const isActivating = actionLoading === `activate-endpoint-${endpoint.id}`;
  const isDeleting = actionLoading === `delete-endpoint-${endpoint.id}`;
  const summary = summarizeEndpoint(endpoint.baseURL);
  const displayName = endpoint.name || '未命名端点';

  const handleToggleActivate = (event) => {
    event.stopPropagation();
    if (endpoint.isActive || isActivating) return;
    onActivate?.(endpoint);
  };

  const handleEdit = (event) => {
    event.stopPropagation();
    onEdit?.(endpoint);
  };

  const handleDelete = (event) => {
    event.stopPropagation();
    if (isDeleting) return;
    onDelete?.(endpoint);
  };

  return (
    <div className="border-b border-slate-100 last:border-b-0">
      <div
        className={`group/row flex items-center gap-3 px-4 py-2 transition-colors duration-150 ${
          endpoint.isActive ? 'bg-emerald-50/30' : 'hover:bg-slate-50/80'
        }`}
      >
        {/* 激活 Toggle（Apple HIG 风格） */}
        <div
          onClick={handleToggleActivate}
          className={`flex-shrink-0 ${endpoint.isActive ? 'cursor-default' : 'cursor-pointer'}`}
          title={endpoint.isActive ? '当前激活端点' : '点击设为当前端点'}
        >
          <div
            className={`relative h-[18px] w-8 rounded-full transition-colors duration-200 ease-out ${
              endpoint.isActive
                ? 'bg-emerald-500'
                : isActivating
                  ? 'bg-emerald-300'
                  : 'bg-slate-200 group-hover/row:bg-slate-300'
            }`}
          >
            <div
              className={`absolute top-[2px] h-[14px] w-[14px] rounded-full bg-white shadow-[0_1px_2px_rgba(15,23,42,0.15)] transition-all duration-200 ease-out ${
                endpoint.isActive ? 'left-[16px]' : 'left-[2px]'
              } ${isActivating ? 'animate-pulse' : ''}`}
            />
          </div>
        </div>

        {/* 端点摘要：name 在前，URL 在后 */}
        <div className="min-w-0 flex-1">
          <div className="flex items-center gap-2">
            <span
              className={`truncate text-xs font-semibold ${
                endpoint.isActive ? 'text-slate-900' : 'text-slate-700'
              }`}
              title={displayName}
            >
              {displayName}
            </span>
            {endpoint.isActive && (
              <span className="text-[10px] uppercase tracking-[0.16em] text-emerald-600">active</span>
            )}
          </div>
          <div
            className={`mt-0.5 truncate font-mono text-[11px] ${
              endpoint.isActive ? 'text-slate-500' : 'text-slate-400'
            }`}
            title={endpoint.baseURL}
          >
            {summary || '<未设置 base_url>'}
          </div>
        </div>

        {/* 操作按钮（hover 浮现） */}
        <div className="flex flex-shrink-0 items-center gap-0.5 opacity-0 transition-opacity duration-150 group-hover/row:opacity-100 focus-within:opacity-100">
          <button
            type="button"
            onClick={handleEdit}
            className="rounded-md p-1 text-slate-400 transition-colors duration-150 hover:bg-slate-100 hover:text-indigo-600"
            title="编辑端点"
          >
            <Pencil size={12} />
          </button>
          <button
            type="button"
            onClick={handleDelete}
            disabled={isDeleting}
            className="rounded-md p-1 text-slate-400 transition-colors duration-150 hover:bg-rose-50 hover:text-rose-600 disabled:opacity-50"
            title="删除端点"
          >
            {isDeleting ? <Loader2 size={12} className="animate-spin" /> : <Trash2 size={12} />}
          </button>
        </div>
      </div>
    </div>
  );
};

// ============================================================================
// 渠道卡片（紧凑小卡片：仅展示渠道配置，不显示具体端点 URL）
// ============================================================================
const ChannelCard = ({
  channel,
  actionLoading,
  onActivateChannel,
  onEditChannel,
  onDeleteChannel,
  onAddEndpoint,
  onActivateEndpoint,
  onEditEndpoint,
  onDeleteEndpoint
}) => {
  const [isEndpointListExpanded, setIsEndpointListExpanded] = useState(false);
  const endpointListRef = useRef(null);
  const isSwitchingChannel = actionLoading === `activate-channel-${channel.id}`;
  const isDeletingChannel = actionLoading === `delete-channel-${channel.id}`;
  const endpoints = channel.endpoints || [];
  const activeEndpoint = endpoints.find((ep) => ep.isActive) || null;
  const hasMultiple = endpoints.length > 1;

  // 折叠时展示哪些行：多端点 → 仅当前激活（无激活则取第一个作为占位）；单端点 → 直接显示
  const displayedEndpoints = isEndpointListExpanded || !hasMultiple
    ? endpoints
    : (activeEndpoint ? [activeEndpoint] : endpoints.slice(0, 1));
  const hiddenCount = hasMultiple && !isEndpointListExpanded ? endpoints.length - displayedEndpoints.length : 0;

  // 外部点击收起
  useEffect(() => {
    if (!isEndpointListExpanded) return undefined;
    const handlePointerDown = (event) => {
      if (endpointListRef.current && !endpointListRef.current.contains(event.target)) {
        setIsEndpointListExpanded(false);
      }
    };
    document.addEventListener('mousedown', handlePointerDown);
    document.addEventListener('touchstart', handlePointerDown);
    return () => {
      document.removeEventListener('mousedown', handlePointerDown);
      document.removeEventListener('touchstart', handlePointerDown);
    };
  }, [isEndpointListExpanded]);

  return (
    <div
      className={`group flex flex-col overflow-hidden rounded-xl border bg-white transition-all duration-150 ease-out shadow-[0_1px_2px_rgba(15,23,42,0.04)] hover:shadow-[0_4px_12px_-2px_rgba(15,23,42,0.08)] ${
        channel.isActive
          ? 'border-emerald-200 ring-1 ring-emerald-100'
          : 'border-slate-200/70 hover:border-slate-300'
      }`}
    >
      {/* 卡片头部：渠道名 + 标签 + 操作 */}
      <div className="flex items-center justify-between gap-3 px-4 py-3">
        <div className="flex min-w-0 flex-1 items-center gap-2">
          <h3 className="truncate text-sm font-semibold tracking-tight text-slate-900">{channel.name}</h3>
          {channel.isActive && (
            <span className="inline-flex flex-shrink-0 items-center gap-1 rounded-full bg-emerald-50 px-1.5 py-0.5 text-[10px] font-medium tracking-tight text-emerald-700">
              <span className="h-1 w-1 rounded-full bg-emerald-500" />
              当前接管
            </span>
          )}
        </div>

        <div className="flex flex-shrink-0 items-center gap-0.5">
          {!channel.isActive && (
            <button
              type="button"
              onClick={() => onActivateChannel?.(channel)}
              disabled={isSwitchingChannel}
              className="inline-flex items-center gap-1 rounded-md px-2 py-1 text-[11px] font-medium text-indigo-600 transition-colors duration-150 hover:bg-indigo-50 disabled:opacity-50"
              title="设为当前接管渠道"
            >
              {isSwitchingChannel ? <Loader2 size={12} className="animate-spin" /> : <ArrowRightLeft size={12} />}
              设为当前
            </button>
          )}
          {channel.website && (
            <button
              type="button"
              onClick={() => openExternalURL(channel.website)}
              className="rounded-md p-1.5 text-slate-400 transition-colors duration-150 hover:bg-slate-100 hover:text-slate-700"
              title="打开官网"
            >
              <Globe size={14} />
            </button>
          )}
          <button
            type="button"
            onClick={() => onEditChannel?.(channel)}
            className="rounded-md p-1.5 text-slate-400 transition-colors duration-150 hover:bg-slate-100 hover:text-indigo-600"
            title="编辑渠道"
          >
            <Pencil size={14} />
          </button>
          <button
            type="button"
            onClick={() => onDeleteChannel?.(channel)}
            disabled={isDeletingChannel}
            className="rounded-md p-1.5 text-slate-400 transition-colors duration-150 hover:bg-rose-50 hover:text-rose-600 disabled:opacity-50"
            title="删除渠道"
          >
            {isDeletingChannel ? <Loader2 size={14} className="animate-spin" /> : <Trash2 size={14} />}
          </button>
        </div>
      </div>

      {/* 元数据条：模型 · 推理 · 1M · API Key（host 显示在端点行，避免重复+换行） */}
      <div className="flex flex-nowrap items-center gap-x-3 gap-y-1.5 overflow-hidden border-t border-slate-100 px-4 py-2 text-[11px]">
        <span className="inline-flex flex-shrink-0 items-center gap-1 text-slate-600">
          <ShieldCheck size={11} className="text-emerald-500" />
          <span className="font-medium">{channel.model || '未设置'}</span>
        </span>
        <span className="flex-shrink-0 text-slate-300">·</span>
        <span className="flex-shrink-0 text-slate-500">
          推理 <span className="font-medium text-amber-600">{channel.reasoningEffort || 'xhigh'}</span>
        </span>
        {channel.enable1MContext && (
          <>
            <span className="flex-shrink-0 text-slate-300">·</span>
            <span className="flex-shrink-0 font-medium text-sky-600">1M</span>
          </>
        )}
        <span className="flex-shrink-0 text-slate-300">·</span>
        <span className="inline-flex min-w-0 flex-1 items-center gap-1 text-slate-500">
          <KeyRound size={11} className="flex-shrink-0 text-slate-400" />
          <span className="truncate font-mono text-[10px]">{channel.apiKeyMasked || '未配置'}</span>
        </span>
      </div>

      {/* 端点子列表 */}
      <div ref={endpointListRef} className="border-t border-slate-100 bg-slate-50/30">
        <div className="flex items-center justify-between px-4 py-1.5">
          <div className="flex items-center gap-2 text-[10px] font-semibold uppercase tracking-[0.16em] text-slate-400">
            <span>Endpoints · {endpoints.length}</span>
            {hasMultiple && (
              <button
                type="button"
                onClick={() => setIsEndpointListExpanded((prev) => !prev)}
                className="inline-flex items-center gap-0.5 rounded-md px-1 py-0.5 text-[10px] font-medium normal-case tracking-normal text-slate-500 transition-colors duration-150 hover:bg-slate-100 hover:text-slate-700"
                title={isEndpointListExpanded ? '收起端点列表' : '展开全部端点'}
              >
                {isEndpointListExpanded ? '收起' : `展开 ${hiddenCount} 个`}
                <ChevronDown
                  size={11}
                  className={`transition-transform duration-200 ease-out ${
                    isEndpointListExpanded ? 'rotate-180' : ''
                  }`}
                />
              </button>
            )}
          </div>
          <button
            type="button"
            onClick={() => onAddEndpoint?.(channel)}
            className="inline-flex items-center gap-1 rounded-md px-1.5 py-0.5 text-[11px] font-medium text-slate-500 transition-colors duration-150 hover:bg-slate-100 hover:text-slate-700"
          >
            <Plus size={12} />
            添加端点
          </button>
        </div>

        {endpoints.length === 0 ? (
          <div className="border-t border-slate-100 bg-white px-4 py-6 text-center text-[11px] text-slate-400">
            暂无端点。请添加至少一个 base_url 以启用切换与激活。
          </div>
        ) : (
          <div className="border-t border-slate-100 bg-white">
            {displayedEndpoints.map((endpoint) => (
              <EndpointRow
                key={endpoint.id}
                endpoint={endpoint}
                onActivate={onActivateEndpoint}
                onEdit={onEditEndpoint}
                onDelete={(ep) => onDeleteEndpoint?.(ep, channel)}
                actionLoading={actionLoading}
              />
            ))}
          </div>
        )}
      </div>
    </div>
  );
};

// ============================================================================
// 主页面
// ============================================================================
const CodexPage = () => {
  const [channels, setChannels] = useState([]);
  const [storageStatus, setStorageStatus] = useState(null);
  const [loading, setLoading] = useState(true);
  const [actionLoading, setActionLoading] = useState('');
  const [error, setError] = useState('');
  const [lastUpdate, setLastUpdate] = useState('');

  // 渠道弹窗
  const [showChannelForm, setShowChannelForm] = useState(false);
  const [channelFormMode, setChannelFormMode] = useState('create');
  const [editingChannel, setEditingChannel] = useState(null);
  const [channelFormError, setChannelFormError] = useState('');
  const [deleteChannelTarget, setDeleteChannelTarget] = useState(null);

  // 端点弹窗（创建 + 编辑共用 EndpointFormModal）
  const [showEndpointForm, setShowEndpointForm] = useState(false);
  const [endpointFormMode, setEndpointFormMode] = useState('create'); // 'create' | 'edit'
  const [endpointFormChannel, setEndpointFormChannel] = useState(null);
  const [editingEndpoint, setEditingEndpoint] = useState(null);
  const [endpointFormError, setEndpointFormError] = useState('');
  const [deleteEndpointTarget, setDeleteEndpointTarget] = useState(null);

  const activeChannel = channels.find((channel) => channel.isActive) || null;
  const activeEndpoint = activeChannel?.endpoints?.find((ep) => ep.isActive) || null;

  const overviewCards = [
    {
      title: '当前接管',
      value: activeChannel?.name || '未设置',
      tooltip: activeChannel?.name || '未设置',
      subText: activeChannel ? '已接管 config.toml / auth.json' : '请选择一个渠道设为当前',
      icon: CheckCircle2,
      statusColor: 'bg-emerald-50 text-emerald-600'
    },
    {
      title: '当前模型',
      value: activeChannel?.model || '等待接管',
      tooltip: activeChannel?.model || '等待接管',
      subText: activeChannel
        ? `推理强度 ${activeChannel.reasoningEffort || 'xhigh'}${activeChannel.enable1MContext ? ' · 1M 上下文' : ''}`
        : '接管后自动同步模型参数',
      icon: ShieldCheck,
      statusColor: 'bg-sky-50 text-sky-600'
    },
    {
      title: '当前端点',
      value: activeEndpoint ? (activeEndpoint.name || '未命名端点') : '未激活',
      tooltip: activeEndpoint?.baseURL || '未激活端点',
      subText: activeEndpoint
        ? summarizeEndpoint(activeEndpoint.baseURL)
        : '请激活一个端点以接管 base_url',
      icon: Globe,
      statusColor: 'bg-indigo-50 text-indigo-600'
    },
    {
      title: '已保存渠道',
      value: String(storageStatus?.totalCount ?? channels.length),
      tooltip: String(storageStatus?.totalCount ?? channels.length),
      subText: '数据库中维护的渠道数量',
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

  const fetchData = useCallback(async ({ silent = false } = {}) => {
    if (!silent) setLoading(true);
    setError('');
    try {
      const [list, status] = await Promise.all([
        getCodexChannels(),
        getCodexProviderStorageStatus().catch(() => null)
      ]);
      setChannels(list);
      if (status) setStorageStatus(status);
      setLastUpdate(new Date().toLocaleTimeString());
    } catch (err) {
      setError(getErrorMessage(err, '加载 Codex 渠道失败'));
    } finally {
      if (!silent) setLoading(false);
    }
  }, []);

  useEffect(() => {
    fetchData();
    const unsubscribe = subscribeToEvent('codex_provider_updated', () => {
      fetchData({ silent: true });
    });
    return () => {
      if (typeof unsubscribe === 'function') unsubscribe();
    };
  }, [fetchData]);

  // ===== 渠道动作 =====
  const handleActivateChannel = async (channel) => {
    if (channel.isActive) return;
    setActionLoading(`activate-channel-${channel.id}`);
    try {
      await activateCodexChannel(channel.id);
      await fetchData({ silent: true });
    } catch (err) {
      setError(getErrorMessage(err, '切换渠道失败'));
    } finally {
      setActionLoading('');
    }
  };

  const openCreateChannelForm = () => {
    setChannelFormMode('create');
    setEditingChannel(null);
    setChannelFormError('');
    setShowChannelForm(true);
  };

  const openEditChannelForm = (channel) => {
    setChannelFormMode('edit');
    setEditingChannel(channel);
    setChannelFormError('');
    setShowChannelForm(true);
  };

  const closeChannelForm = () => {
    setShowChannelForm(false);
    setEditingChannel(null);
    setChannelFormError('');
  };

  const handleSaveChannel = async (payload) => {
    setActionLoading(channelFormMode === 'edit' ? 'update-channel' : 'create-channel');
    setChannelFormError('');
    try {
      if (channelFormMode === 'edit' && editingChannel) {
        await updateCodexChannelByID(editingChannel.id, payload);
      } else {
        await createCodexChannel(payload);
      }
      await fetchData({ silent: true });
      closeChannelForm();
    } catch (err) {
      setChannelFormError(getErrorMessage(err, '保存渠道失败'));
    } finally {
      setActionLoading('');
    }
  };

  const handleDeleteChannel = async () => {
    if (!deleteChannelTarget) return;
    setActionLoading(`delete-channel-${deleteChannelTarget.id}`);
    try {
      await deleteCodexChannelByID(deleteChannelTarget.id);
      await fetchData({ silent: true });
      setDeleteChannelTarget(null);
    } catch (err) {
      setError(getErrorMessage(err, '删除渠道失败'));
    } finally {
      setActionLoading('');
    }
  };

  // ===== 端点动作 =====
  const openCreateEndpointForm = (channel) => {
    setEndpointFormMode('create');
    setEndpointFormChannel(channel);
    setEditingEndpoint(null);
    setEndpointFormError('');
    setShowEndpointForm(true);
  };

  const openEditEndpointForm = (endpoint) => {
    const channel = channels.find((c) => c.id === endpoint.channelId) || null;
    setEndpointFormMode('edit');
    setEndpointFormChannel(channel);
    setEditingEndpoint(endpoint);
    setEndpointFormError('');
    setShowEndpointForm(true);
  };

  const closeEndpointForm = () => {
    setShowEndpointForm(false);
    setEndpointFormChannel(null);
    setEditingEndpoint(null);
    setEndpointFormError('');
  };

  const handleSaveEndpoint = async ({ name, baseURL }) => {
    if (endpointFormMode === 'edit') {
      if (!editingEndpoint) return;
      setActionLoading(`save-endpoint-${editingEndpoint.id}`);
      setEndpointFormError('');
      try {
        await updateCodexEndpointByID(editingEndpoint.id, { name, baseURL });
        await fetchData({ silent: true });
        closeEndpointForm();
      } catch (err) {
        setEndpointFormError(getErrorMessage(err, '更新端点失败'));
      } finally {
        setActionLoading('');
      }
      return;
    }

    if (!endpointFormChannel) return;
    setActionLoading('create-endpoint');
    setEndpointFormError('');
    try {
      await createCodexEndpoint({
        channelId: endpointFormChannel.id,
        name,
        baseURL
      });
      await fetchData({ silent: true });
      closeEndpointForm();
    } catch (err) {
      setEndpointFormError(getErrorMessage(err, '保存端点失败'));
    } finally {
      setActionLoading('');
    }
  };

  const handleActivateEndpoint = async (endpoint) => {
    if (endpoint.isActive) return;
    setActionLoading(`activate-endpoint-${endpoint.id}`);
    try {
      await activateCodexEndpoint(endpoint.id);
      await fetchData({ silent: true });
    } catch (err) {
      setError(getErrorMessage(err, '激活端点失败'));
    } finally {
      setActionLoading('');
    }
  };

  const handleDeleteEndpoint = async () => {
    if (!deleteEndpointTarget) return;
    const epId = deleteEndpointTarget.endpoint.id;
    setActionLoading(`delete-endpoint-${epId}`);
    try {
      await deleteCodexEndpointByID(epId);
      await fetchData({ silent: true });
      setDeleteEndpointTarget(null);
    } catch (err) {
      setError(getErrorMessage(err, '删除端点失败'));
    } finally {
      setActionLoading('');
    }
  };

  return (
    <div className="animate-fade-in">
      {/* 页面标题 */}
      <div className="flex justify-between items-end mb-8">
        <div className="flex items-center gap-3">
          <div className="p-2 bg-slate-900 rounded-lg text-white shadow-lg">
            <CodexLogo className="w-6 h-6" />
          </div>
          <div>
            <h1 className="text-2xl font-bold text-slate-900">Codex 管理</h1>
            <p className="text-slate-500 text-sm mt-1">
              渠道承载共用配置，端点承载 base_url；切换激活端点时仅 config.toml 中 base_url 一行改写
              {lastUpdate && (
                <span className="ml-2 text-slate-400">· 更新于 {lastUpdate}</span>
              )}
            </p>
          </div>
        </div>
        <div className="flex items-center gap-3">
          <Button
            variant="ghost"
            size="sm"
            icon={RefreshCw}
            onClick={() => fetchData()}
            loading={loading}
          >
            刷新
          </Button>
          <Button icon={Plus} onClick={openCreateChannelForm}>
            添加渠道
          </Button>
        </div>
      </div>

      {/* 统计卡片 */}
      <div className="mb-6 grid grid-cols-1 gap-3 sm:grid-cols-2 xl:grid-cols-5">
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
            size="lg"
          />
        ))}
      </div>

      {error && (
        <div className="mb-4">
          <ErrorMessage message={error} onClose={() => setError('')} />
        </div>
      )}

      {loading ? (
        <div className="flex items-center justify-center py-16">
          <LoadingSpinner />
        </div>
      ) : channels.length === 0 ? (
        <EmptyState
          icon={Database}
          title="暂无 Codex 渠道"
          description="请先创建一个渠道并为其添加至少一个端点（base_url），随后即可在管理界面中切换激活。"
          action={
            <Button variant="primary" icon={Plus} onClick={openCreateChannelForm}>
              添加渠道
            </Button>
          }
        />
      ) : (
        <div className="grid grid-cols-1 items-start gap-4 md:grid-cols-2 xl:grid-cols-3">
          {channels.map((channel) => (
            <ChannelCard
              key={channel.id}
              channel={channel}
              actionLoading={actionLoading}
              onActivateChannel={handleActivateChannel}
              onEditChannel={openEditChannelForm}
              onDeleteChannel={(c) => setDeleteChannelTarget(c)}
              onAddEndpoint={openCreateEndpointForm}
              onActivateEndpoint={handleActivateEndpoint}
              onEditEndpoint={openEditEndpointForm}
              onDeleteEndpoint={(endpoint, channelOwner) =>
                setDeleteEndpointTarget({ endpoint, channelOwner })
              }
            />
          ))}
        </div>
      )}

      {showChannelForm && (
        <ChannelFormModal
          mode={channelFormMode}
          channel={editingChannel}
          loading={actionLoading === 'create-channel' || actionLoading === 'update-channel'}
          serverError={channelFormError}
          onCancel={closeChannelForm}
          onSave={handleSaveChannel}
        />
      )}

      {showEndpointForm && (
        <EndpointFormModal
          mode={endpointFormMode}
          channel={endpointFormChannel}
          endpoint={editingEndpoint}
          loading={
            endpointFormMode === 'edit'
              ? actionLoading === `save-endpoint-${editingEndpoint?.id}`
              : actionLoading === 'create-endpoint'
          }
          serverError={endpointFormError}
          onCancel={closeEndpointForm}
          onSave={handleSaveEndpoint}
        />
      )}

      {deleteChannelTarget && (
        <DeleteConfirmDialog
          title={`删除渠道「${deleteChannelTarget.name}」`}
          description="该操作会级联删除其下所有端点，且无法撤销。"
          hint={
            deleteChannelTarget.isActive
              ? '该渠道是当前接管渠道，删除后系统会尝试激活下一个渠道并同步 config.toml。'
              : '删除后端点也会一并消失。若需保留端点请先迁移到其他渠道。'
          }
          loading={actionLoading === `delete-channel-${deleteChannelTarget.id}`}
          onCancel={() => setDeleteChannelTarget(null)}
          onConfirm={handleDeleteChannel}
        />
      )}

      {deleteEndpointTarget && (
        <DeleteConfirmDialog
          title="删除端点"
          description={`即将从渠道「${deleteEndpointTarget.channelOwner?.name}」中移除该 base_url。`}
          hint={
            deleteEndpointTarget.endpoint.isActive
              ? '这是该渠道的当前激活端点；删除后会自动激活该渠道下一个端点（若有）。'
              : '该端点未激活，删除不会触发 config.toml 改写。'
          }
          loading={actionLoading === `delete-endpoint-${deleteEndpointTarget.endpoint.id}`}
          onCancel={() => setDeleteEndpointTarget(null)}
          onConfirm={handleDeleteEndpoint}
        />
      )}
    </div>
  );
};

export default CodexPage;
