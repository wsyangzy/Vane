// ============================================
// Codex 页面 - Provider 配置接管管理
// 2026-03-10
// ============================================

import { useCallback, useEffect, useState } from 'react';
import {
  ArrowRightLeft,
  CheckCircle2,
  Database,
  Globe,
  KeyRound,
  Pencil,
  Plus,
  RefreshCw,
  Server,
  ShieldCheck,
  Trash2,
  TriangleAlert
} from 'lucide-react';
import {
  Button,
  EmptyState,
  ErrorMessage,
  Input,
  LoadingSpinner,
  Select
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
  { value: 'openai-compatible', label: 'OpenAI Compatible' },
  { value: 'azure-openai', label: 'Azure OpenAI' },
  { value: 'custom', label: 'Custom' }
];

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
  website: provider?.website || ''
});

const StatCard = ({ title, value, detail, icon: Icon, accentClass }) => (
  <div className="bg-white rounded-2xl border border-slate-200/60 p-5 shadow-sm">
    <div className="flex items-start justify-between gap-3">
      <div>
        <div className="text-xs font-semibold uppercase tracking-[0.16em] text-slate-400">{title}</div>
        <div className="mt-2 text-3xl font-bold text-slate-900">{value}</div>
        <div className="mt-2 text-sm text-slate-500">{detail}</div>
      </div>
      <div className={`rounded-xl p-3 ${accentClass}`}>
        <Icon size={20} />
      </div>
    </div>
  </div>
);

const ProviderFormModal = ({ mode, provider, loading, serverError, onCancel, onSave }) => {
  const [formData, setFormData] = useState(() => buildInitialForm(provider));
  const [fieldError, setFieldError] = useState('');

  useEffect(() => {
    setFormData(buildInitialForm(provider));
    setFieldError('');
  }, [provider, mode]);

  const updateField = (key, value) => {
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
      website: formData.website.trim()
    };

    if (!payload.name || !payload.providerType || !payload.baseURL || !payload.apiKey || !payload.model) {
      setFieldError('请填写名称、Provider 类型、Base URL、API Key 和默认模型');
      return;
    }

    await onSave?.(payload);
  };

  return (
    <div className="fixed inset-0 z-50 flex items-start justify-center bg-black/50 px-4 pt-[10vh] animate-fade-in">
      <div className="w-full max-w-2xl rounded-2xl bg-white shadow-xl">
        <div className="border-b border-slate-100 px-6 py-5">
          <div className="flex items-center gap-3">
            <div className="rounded-xl bg-slate-900 p-2 text-white shadow-lg">
              {mode === 'create' ? <Plus size={18} /> : <Pencil size={18} />}
            </div>
            <div>
              <h2 className="text-lg font-semibold text-slate-900">
                {mode === 'create' ? '添加 Codex Provider' : '编辑 Codex Provider'}
              </h2>
              <p className="mt-1 text-sm text-slate-500">
                这里只维护接管配置与当前 Provider 切换，不参与故障转移。
              </p>
            </div>
          </div>
        </div>

        <form className="space-y-5 px-6 py-6" onSubmit={handleSubmit}>
          <div className="grid grid-cols-1 gap-5 md:grid-cols-2">
            <Input
              label="Provider 名称"
              value={formData.name}
              onChange={(event) => updateField('name', event.target.value)}
              placeholder="例如：OpenAI 主通道"
              autoFocus
            />

            <Select
              label="Provider 类型"
              value={formData.providerType}
              onChange={(event) => updateField('providerType', event.target.value)}
              options={PROVIDER_TYPE_OPTIONS}
            />

            <Input
              label="Base URL"
              value={formData.baseURL}
              onChange={(event) => updateField('baseURL', event.target.value)}
              placeholder="https://api.example.com/v1"
            />

            <Input
              label="默认模型"
              value={formData.model}
              onChange={(event) => updateField('model', event.target.value)}
              placeholder="gpt-4.1 / codex-mini-latest"
            />

            <div className="md:col-span-2">
              <Input
                label="API Key"
                value={formData.apiKey}
                onChange={(event) => updateField('apiKey', event.target.value)}
                placeholder="sk-..."
              />
            </div>

            <div className="md:col-span-2">
              <Input
                label="官网地址（可选）"
                value={formData.website}
                onChange={(event) => updateField('website', event.target.value)}
                placeholder="https://platform.openai.com"
              />
            </div>
          </div>

          {(fieldError || serverError) && (
            <div className="rounded-xl border border-rose-200 bg-rose-50 px-4 py-3 text-sm text-rose-700">
              {fieldError || serverError}
            </div>
          )}

          <div className="flex justify-end gap-3 border-t border-slate-100 pt-5">
            <Button type="button" variant="ghost" onClick={onCancel} disabled={loading}>
              取消
            </Button>
            <Button type="submit" icon={mode === 'create' ? Plus : Pencil} loading={loading}>
              {mode === 'create' ? '创建 Provider' : '保存修改'}
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
    <div className="fixed inset-0 z-50 flex items-start justify-center bg-black/50 px-4 pt-[18vh] animate-fade-in">
      <div className="w-full max-w-md rounded-2xl bg-white p-6 shadow-xl">
        <div className="mb-4 flex items-center gap-3">
          <div className="rounded-full bg-rose-100 p-3">
            <TriangleAlert className="text-rose-600" size={24} />
          </div>
          <div>
            <h3 className="text-lg font-semibold text-slate-900">确认删除 Provider</h3>
            <p className="text-sm text-slate-500">此操作不会处理请求追踪或计费数据</p>
          </div>
        </div>

        <p className="mb-5 text-sm leading-6 text-slate-600">
          确定要删除 <span className="font-semibold text-slate-900">“{provider.name}”</span> 吗？
          删除后需要重新选择接管配置。
        </p>

        <div className="flex justify-end gap-3">
          <Button variant="ghost" onClick={onCancel} disabled={loading}>
            取消
          </Button>
          <Button variant="danger" icon={Trash2} loading={loading} onClick={onConfirm}>
            删除 Provider
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
      className={`rounded-2xl border bg-white p-5 shadow-sm transition-shadow hover:shadow-md ${
        provider.isActive ? 'border-emerald-200 shadow-emerald-100/40' : 'border-slate-200/60'
      }`}
    >
      <div className="flex items-start justify-between gap-4">
        <div className="min-w-0">
          <div className="flex flex-wrap items-center gap-2">
            <h3 className="text-lg font-semibold text-slate-900">{provider.name}</h3>
            <span
              className={`inline-flex items-center rounded-full border px-2.5 py-1 text-xs font-medium ${
                provider.isActive
                  ? 'border-emerald-200 bg-emerald-50 text-emerald-700'
                  : 'border-slate-200 bg-slate-50 text-slate-500'
              }`}
            >
              {provider.isActive ? '当前接管中' : '待切换'}
            </span>
          </div>
          <div className="mt-3 flex flex-wrap items-center gap-2 text-xs">
            <span className="inline-flex items-center rounded-full border border-indigo-200 bg-indigo-50 px-2.5 py-1 font-medium text-indigo-700">
              <Server size={12} className="mr-1.5" />
              {formatProviderType(provider.providerType)}
            </span>
            <span className="inline-flex items-center rounded-full border border-amber-200 bg-amber-50 px-2.5 py-1 font-medium text-amber-700">
              <ShieldCheck size={12} className="mr-1.5" />
              {provider.model}
            </span>
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

      <div className="mt-5 grid grid-cols-1 gap-3 md:grid-cols-2">
        <div className="rounded-xl border border-slate-100 bg-slate-50/70 p-4">
          <div className="text-xs font-semibold uppercase tracking-[0.14em] text-slate-400">Base URL</div>
          <div className="mt-2 truncate font-mono text-sm text-slate-700" title={provider.baseURL}>
            {provider.baseURL}
          </div>
        </div>

        <div className="rounded-xl border border-slate-100 bg-slate-50/70 p-4">
          <div className="text-xs font-semibold uppercase tracking-[0.14em] text-slate-400">API Key</div>
          <div className="mt-2 flex items-center font-mono text-sm text-slate-700">
            <KeyRound size={14} className="mr-2 text-slate-400" />
            {provider.apiKeyMasked || '未显示'}
          </div>
        </div>
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
            {provider.isActive ? '当前 Provider' : '设为当前'}
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
  const providerTypeCount = new Set(providers.map((provider) => provider.providerType).filter(Boolean)).size;
  const pendingSwitchCount = activeProvider ? Math.max(providers.length - 1, 0) : providers.length;

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
      setError(getErrorMessage(loadError, '加载 Codex Provider 失败'));
    } finally {
      if (!silent) {
        setLoading(false);
      }
    }
  }, []);

  useEffect(() => {
    if (!isWailsEnvironment()) {
      setError('当前环境未启用 Wails，无法管理 Codex Provider');
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
      setFormError(getErrorMessage(saveError, '保存 Codex Provider 失败'));
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
      setError(getErrorMessage(activateError, '切换 Codex Provider 失败'));
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
      setError(getErrorMessage(deleteError, '删除 Codex Provider 失败'));
    } finally {
      setActionLoading('');
    }
  };

  if (loading && !storageStatus) {
    return <LoadingSpinner text="加载 Codex Provider..." />;
  }

  if (error && !storageStatus && providers.length === 0) {
    return (
      <ErrorMessage
        title="Codex Provider 加载失败"
        message={error}
        onRetry={handleRefresh}
      />
    );
  }

  return (
    <div className="animate-fade-in">
      <div className="mb-8 flex items-end justify-between gap-4">
        <div className="flex items-center gap-3">
          <div className="rounded-xl bg-slate-900 p-2 text-white shadow-lg">
            <Database className="h-6 w-6" />
          </div>
          <div>
            <h1 className="text-2xl font-bold text-slate-900">Codex 管理</h1>
            <p className="mt-1 text-sm text-slate-500">
              独立维护 Codex Provider 配置，只做接管切换，不参与渠道间或渠道内故障转移
              {lastUpdate && <span className="ml-2 text-slate-400">· 更新于 {lastUpdate}</span>}
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
          <Button icon={Plus} onClick={handleCreate}>
            添加 Provider
          </Button>
        </div>
      </div>

      <div className="mb-6 grid grid-cols-1 gap-4 md:grid-cols-2 xl:grid-cols-4">
        <StatCard
          title="Provider 总数"
          value={storageStatus?.totalCount || providers.length}
          detail={storageStatus?.enabled ? '已启用独立 SQLite 存储' : '存储未就绪'}
          icon={Database}
          accentClass="bg-slate-100 text-slate-700"
        />
        <StatCard
          title="当前接管"
          value={activeProvider ? activeProvider.name : '未设置'}
          detail={activeProvider ? activeProvider.model : '请选择一个 Provider 设为当前'}
          icon={CheckCircle2}
          accentClass="bg-emerald-100 text-emerald-700"
        />
        <StatCard
          title="Provider 类型"
          value={providerTypeCount}
          detail={providerTypeCount > 0 ? '支持跨提供商切换' : '尚未配置提供商'}
          icon={Server}
          accentClass="bg-indigo-100 text-indigo-700"
        />
        <StatCard
          title="待切换项"
          value={pendingSwitchCount}
          detail={pendingSwitchCount > 0 ? '可作为下一次接管目标' : '当前没有备选项'}
          icon={ArrowRightLeft}
          accentClass="bg-amber-100 text-amber-700"
        />
      </div>

      {error && (
        <div className="mb-6 flex items-start gap-3 rounded-2xl border border-rose-200 bg-rose-50 px-4 py-4 text-sm text-rose-700">
          <TriangleAlert className="mt-0.5 h-4 w-4 flex-shrink-0" />
          <div>{error}</div>
        </div>
      )}

      {providers.length > 0 && !storageStatus?.hasActive && (
        <div className="mb-6 flex items-start gap-3 rounded-2xl border border-amber-200 bg-amber-50 px-4 py-4 text-sm text-amber-700">
          <TriangleAlert className="mt-0.5 h-4 w-4 flex-shrink-0" />
          <div>当前没有正在接管的 Provider，请从列表中选择一个设为当前。</div>
        </div>
      )}

      {providers.length === 0 ? (
        <div className="rounded-2xl border border-slate-200/60 bg-white shadow-sm">
          <EmptyState
            icon={Database}
            title="暂无 Codex Provider"
            description="先添加一个 Provider 作为接管配置，后续即可在不同提供商之间切换。"
            action={
              <Button icon={Plus} onClick={handleCreate}>
                添加第一个 Provider
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

