import { useState } from 'react';
import { Database, KeyRound, Pencil, Save, X } from 'lucide-react';
import { Button, CustomSelect, SegmentedTabs, Tooltip } from '@/shared/ui/index.js';
import { renderModalPortal } from '@/shared/ui/modalPortal.js';
import { isSubscriptionGroup } from '@/features/providers/shared/subscriptions/subscriptionSettings.js';
import { isWailsEnvironment } from '@/services/wails/index.js';
import { formatMultiplierExpression } from '@/shared/lib/multiplier.js';
import { CODEX_ENDPOINT_FORM_TABS, getNextCodexEndpointPriority, BILLING_OUTPUT_TOKEN_SOURCE_OPTIONS, formatEndpointTimeoutInputValue, parseEndpointTimeoutPayload, parseEndpointPriorityPayload } from '../model.js';
import { EndpointToggleTile } from './EndpointToggleTile.jsx';

const EndpointFormModal = ({ mode, channel, endpoint, credentialGroup, loading, serverError, onCancel, onSave }) => {
  const [name, setName] = useState(endpoint?.name || '');
  const [baseURL, setBaseURL] = useState(endpoint?.baseURL || endpoint?.base_url || '');
  const [activeTab, setActiveTab] = useState('basic');
  const credentialGroups = channel?.credentialGroups || [];
  const defaultCredentialGroup =
    credentialGroups.find((group) => group.isDefault || group.is_default) || credentialGroups[0] || null;
  const initialCredentialGroupId =
    endpoint?.credentialGroupId || endpoint?.credential_group_id || credentialGroup?.id || defaultCredentialGroup?.id || 0;
  const [credentialGroupId, setCredentialGroupId] = useState(initialCredentialGroupId);
  const selectedCredentialGroup =
    credentialGroups.find((group) => Number(group.id) === Number(credentialGroupId)) ||
    credentialGroup ||
    defaultCredentialGroup;
  const subscriptionGroup = isSubscriptionGroup(selectedCredentialGroup);
  const selectedCredentialGroupId = Number(selectedCredentialGroup?.id || credentialGroupId || 0);
  const credentialGroupOptions = [
    ...(!credentialGroups.some((group) => Number(group.id) === selectedCredentialGroupId) && selectedCredentialGroupId > 0
      ? [{ value: String(selectedCredentialGroupId), label: `${selectedCredentialGroup?.name || '当前分组'} (当前)` }]
      : []),
    ...credentialGroups.map((group) => ({
      value: String(group.id),
      label: `${group.name || '默认分组'}${group.isDefault || group.is_default ? ' · 默认' : ''}`
    }))
  ];
  const isEdit = mode === 'edit';
  const initialPriority = getNextCodexEndpointPriority(channel, initialCredentialGroupId);
  const [priority, setPriority] = useState(() => (isEdit ? String(endpoint?.priority ?? initialPriority) : ''));
  const [timeoutSeconds, setTimeoutSeconds] = useState(formatEndpointTimeoutInputValue(endpoint?.timeoutSeconds ?? endpoint?.timeout_seconds ?? 0));
  const failoverEnabled = endpoint?.failoverEnabled !== false;
  const [skipHealthCheck, setSkipHealthCheck] = useState(Boolean(endpoint?.skipHealthCheck || endpoint?.skip_health_check));
  const [fieldError, setFieldError] = useState('');
  const isWails = isWailsEnvironment();
  const modalTopPadding = isWails ? 48 : 16;
  const modalBottomPadding = 16;
  const modalMaxHeight = `calc(100vh - ${modalTopPadding + modalBottomPadding}px)`;
  const groupMultiplierItems = [
    ['总', selectedCredentialGroup?.costMultiplier ?? 1],
    ['输入', selectedCredentialGroup?.inputCostMultiplier ?? 1],
    ['输出', selectedCredentialGroup?.outputCostMultiplier ?? 1],
    ['缓存读取', selectedCredentialGroup?.cacheReadCostMultiplier ?? 1],
    ['缓存创建', selectedCredentialGroup?.cacheCreationCostMultiplier ?? 1],
    ['Long context', selectedCredentialGroup?.longContextCostMultiplier ?? 1]
  ];
  const groupBillingSource = selectedCredentialGroup?.billingOutputTokenSource || 'output_tokens';
  const groupBillingLabel = BILLING_OUTPUT_TOKEN_SOURCE_OPTIONS.find((option) => option.value === groupBillingSource)?.label || groupBillingSource;

  const handleSubmit = async (event) => {
    event.preventDefault();
    const trimmedName = name.trim();
    const trimmed = subscriptionGroup ? 'https://chatgpt.com/backend-api/codex' : baseURL.trim();
    if (!trimmedName) {
      setActiveTab('basic');
      setFieldError('端点名称不能为空');
      return;
    }
    if (!trimmed) {
      setActiveTab('basic');
      setFieldError('base_url 不能为空');
      return;
    }
    if (!/^https?:\/\//i.test(trimmed)) {
      setActiveTab('basic');
      setFieldError('base_url 必须以 http:// 或 https:// 开头');
      return;
    }
    const priorityResult = parseEndpointPriorityPayload(priority);
    if (priorityResult.error) {
      setActiveTab('routing');
      setFieldError(priorityResult.error);
      return;
    }
    const timeoutResult = parseEndpointTimeoutPayload(timeoutSeconds);
    if (timeoutResult.error) {
      setActiveTab('routing');
      setFieldError(timeoutResult.error);
      return;
    }
    await onSave?.({
      name: trimmedName,
      baseURL: trimmed,
      credentialGroupId: selectedCredentialGroupId,
      failoverEnabled,
      skipHealthCheck,
      priority: priorityResult.priority,
      timeoutSeconds: timeoutResult.timeoutSeconds
    });
  };

  return renderModalPortal(
    <div
      className="fixed inset-0 z-[100] flex items-start justify-center overflow-y-auto bg-slate-900/30 px-4 pb-4 backdrop-blur-sm animate-fade-in"
      style={{ paddingTop: `${modalTopPadding}px` }}
    >
      <div
        className="flex w-full max-w-2xl flex-col overflow-hidden rounded-2xl bg-white shadow-xl"
        style={{ maxHeight: modalMaxHeight }}
      >
        <div className="flex items-center justify-between border-b border-slate-100 px-6 py-4">
          <h2 className="text-lg font-semibold text-slate-900">{isEdit ? '编辑端点' : '新建端点'}</h2>
          <button
            type="button"
            onClick={onCancel}
            className="rounded-lg p-2 text-slate-400 transition-colors hover:bg-slate-100 hover:text-slate-700"
            disabled={loading}
            aria-label="关闭"
          >
            <X size={20} />
          </button>
        </div>

        <form className="flex min-h-0 flex-1 flex-col overflow-hidden" onSubmit={handleSubmit} autoComplete="off">
          <div className="flex-shrink-0 border-b border-slate-100 bg-slate-50/70 px-6 py-3">
            <SegmentedTabs
              tabs={CODEX_ENDPOINT_FORM_TABS}
              activeTab={activeTab}
              onChange={setActiveTab}
              disabled={loading}
              className="grid-cols-2"
              buttonPaddingClass="px-2 sm:px-3"
            />
          </div>
          <div className="space-y-4 overflow-y-auto px-6 py-6">
            {(fieldError || serverError) && (
              <div className="rounded-lg border border-rose-200 bg-rose-50 px-4 py-3 text-sm text-rose-700">
                {fieldError || serverError}
              </div>
            )}

            <div className="rounded-xl border border-slate-200 bg-slate-50/70 p-3">
              <div className="flex items-center gap-2 text-xs font-medium text-slate-700">
                <KeyRound size={14} />
                归属凭证分组
              </div>
              <div className="mt-2 flex min-w-0 flex-wrap items-center gap-2 text-xs">
                <span className="inline-flex max-w-full items-center gap-1 rounded-md bg-white px-2.5 py-1.5 font-medium text-slate-700">
                  <span className="truncate">{selectedCredentialGroup?.name || '默认分组'}</span>
                  {(selectedCredentialGroup?.isDefault || selectedCredentialGroup?.is_default) && (
                    <span className="rounded bg-emerald-50 px-1 text-[10px] font-semibold text-emerald-600">默认</span>
                  )}
                </span>
                {groupMultiplierItems.map(([label, value]) => (
                  <span key={label} className="inline-flex items-center gap-1 rounded-md bg-white px-2 py-1 font-mono text-[11px] font-semibold text-slate-600">
                    <span className="font-sans text-[10px] font-medium text-slate-400">{label}</span>
                    {formatMultiplierExpression(Number(value) || 1, 3)}x
                  </span>
                ))}
                <span className="inline-flex min-w-0 items-center gap-1 rounded-md bg-white px-2 py-1 text-[11px] font-medium text-slate-500">
                  <Database size={11} />
                  <span className="truncate">{groupBillingLabel}</span>
                </span>
              </div>
            </div>

            {activeTab === 'basic' ? (
              <div className="space-y-4">
                <h3 className="text-sm font-semibold uppercase tracking-wide text-slate-500">基本信息</h3>

                <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
                  <div>
                    <label className="mb-1.5 block text-sm font-medium text-slate-700">
                      渠道 <span className="text-rose-500">*</span>
                    </label>
                    <input
                      type="text"
                      value={channel?.name || ''}
                      disabled
                      className="w-full cursor-not-allowed rounded-lg border border-slate-200 bg-slate-50 px-3 py-2 text-sm text-slate-400"
                    />
                    <p className="mt-1 text-xs text-slate-400">用于分块展示端点，并作为故障转移的第一层边界</p>
                  </div>

                  <div>
                    <label className="mb-1.5 block text-sm font-medium text-slate-700">凭证分组</label>
                    <CustomSelect
                      options={credentialGroupOptions}
                      value={String(selectedCredentialGroupId || '')}
                      onChange={(value) => {
                        const nextCredentialGroupId = Number(value) || 0;
                        setCredentialGroupId(nextCredentialGroupId);
                        if (isEdit) {
                          if (nextCredentialGroupId !== Number(initialCredentialGroupId || 0)) {
                            setPriority(String(getNextCodexEndpointPriority(channel, nextCredentialGroupId)));
                          } else {
                            setPriority(String(endpoint?.priority ?? getNextCodexEndpointPriority(channel, nextCredentialGroupId)));
                          }
                        }
                        if (fieldError) setFieldError('');
                      }}
                      disabled={loading || credentialGroupOptions.length === 0}
                      size="md"
                      fullWidth
                      placeholder="请选择凭证分组"
                    />
                    <p className="mt-1 text-xs text-slate-400">
                      {subscriptionGroup ? '使用该分组所选的订阅账号或客户端登录凭证' : '端点归属到该分组后复用其中的 API Key 与默认倍率'}
                    </p>
                  </div>

                  <div>
                    <label className="mb-1.5 block text-sm font-medium text-slate-700">
                      端点名称 <span className="text-rose-500">*</span>
                    </label>
                    <input
                      type="text"
                      autoFocus
                      value={name}
                      onChange={(event) => {
                        setName(event.target.value);
                        if (fieldError) setFieldError('');
                      }}
                      placeholder="e.g. api-primary"
                      className="w-full rounded-lg border border-slate-200 px-3 py-2 text-sm focus:border-indigo-500 focus:outline-none focus:ring-2 focus:ring-indigo-500/20"
                    />
                    <p className="mt-1 text-xs text-slate-400">同一凭证分组下端点名称唯一，仅作 UI 标识</p>
                  </div>

                  <div>
                    <label className="mb-1.5 block text-sm font-medium text-slate-700">
                      URL <span className="text-rose-500">*</span>
                    </label>
                    <input
                      type="text"
                      value={subscriptionGroup ? 'https://chatgpt.com/backend-api/codex' : baseURL}
                      disabled={loading || subscriptionGroup}
                      onChange={(event) => {
                        setBaseURL(event.target.value);
                        if (fieldError) setFieldError('');
                      }}
                      placeholder="https://api.example.com"
                      className="w-full rounded-lg border border-slate-200 px-3 py-2 font-mono text-sm focus:border-indigo-500 focus:outline-none focus:ring-2 focus:ring-indigo-500/20"
                    />
                    <p className="mt-1 text-xs text-slate-400">
                      {subscriptionGroup ? '订阅端点固定连接官方 Codex 服务，凭证来源在分组中配置' : '端点只保存线路信息；API Key 与计费倍率来自上方凭证分组'}
                    </p>
                  </div>
                </div>
              </div>
            ) : (
              <div className="space-y-4">
                <h3 className="text-sm font-semibold uppercase tracking-wide text-slate-500">路由配置</h3>

                <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
                  <div>
                    <label className="mb-1.5 block text-sm font-medium text-slate-700">优先级</label>
                    <input
                      type="number"
                      min="0"
                      value={priority}
                      onChange={(event) => {
                        setPriority(event.target.value);
                        if (fieldError) setFieldError('');
                      }}
                      disabled={loading}
                      className="w-full rounded-lg border border-slate-200 px-3 py-2 text-sm focus:border-indigo-500 focus:outline-none focus:ring-2 focus:ring-indigo-500/20 disabled:cursor-not-allowed disabled:bg-slate-50 disabled:text-slate-400"
                      placeholder={isEdit ? '0 = 最高优先级' : `留空自动分配 P${getNextCodexEndpointPriority(channel, selectedCredentialGroupId)}`}
                    />
                    <p className="mt-1 text-xs text-slate-400">
                      {isEdit ? '0 = 最高优先级，数字越小越优先，同分组相同则交换' : `留空自动分配为 P${getNextCodexEndpointPriority(channel, selectedCredentialGroupId)}；填写后按填写值保存，重复则替换`}
                    </p>
                  </div>

                  <div>
                    <label className="mb-1.5 block text-sm font-medium text-slate-700">超时时间 (秒)</label>
                    <input
                      type="number"
                      min="0"
                      step="1"
                      value={timeoutSeconds}
                      onChange={(event) => {
                        setTimeoutSeconds(event.target.value);
                        if (fieldError) setFieldError('');
                      }}
                      className="w-full rounded-lg border border-slate-200 px-3 py-2 text-sm focus:border-indigo-500 focus:outline-none focus:ring-2 focus:ring-indigo-500/20"
                      placeholder="默认: 300 (全局配置)"
                    />
                    <p className="mt-1 text-xs text-slate-400">单次请求的最大等待时间，0 使用全局配置</p>
                  </div>
                </div>

                <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
                  <Tooltip content="部分中转商不支持健康检查端点，开启后将跳过定时健康检测" className="block">
                    <EndpointToggleTile
                      label="跳过健康检查"
                      checked={skipHealthCheck}
                      onChange={setSkipHealthCheck}
                      disabled={loading}
                    />
                  </Tooltip>
                </div>
              </div>
            )}
          </div>

          <div className="flex flex-shrink-0 items-center justify-end gap-3 border-t border-slate-100 bg-white px-6 py-4">
            <Button type="button" variant="ghost" onClick={onCancel} disabled={loading}>
              取消
            </Button>
            <Button variant="primary" icon={isEdit ? Pencil : Save} loading={loading} type="submit">
              {isEdit ? '保存修改' : '创建端点'}
            </Button>
          </div>
        </form>
      </div>
    </div>
  );
};

export { EndpointFormModal };
