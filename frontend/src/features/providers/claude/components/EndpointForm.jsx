// ============================================
// 端点编辑表单组件 (SQLite 存储模式)
// (2026-01-10) - 新建时后端自动分配优先级，超时使用全局默认
// ============================================

import { useState } from 'react';
import { Clock3, Database, Hash, KeyRound, Route, ShieldOff, X, Save, AlertCircle } from 'lucide-react';
import { Button, CustomSelect, SegmentedTabs } from '@/shared/ui/index.js';
import { renderModalPortal } from '@/shared/ui/modalPortal.js';
import { getErrorMessage } from '@/features/providers/shared/formatters.js';
import { formatMultiplierExpression } from '@/shared/lib/multiplier.js';
import { isSubscriptionGroup } from '../utils/subscription.js';

const ENDPOINT_FORM_TABS = [
  { key: 'basic', label: '基础信息' },
  { key: 'routing', label: '路由配置' }
];

// ============================================
// 表单输入组件
// ============================================

const FormInput = ({ label, name, value, onChange, type = 'text', placeholder, required, disabled, help, error, inputMode, min, step }) => (
  <div className="space-y-1">
    <label className="block text-sm font-medium text-slate-700">
      {label}
      {required && <span className="text-rose-500 ml-1">*</span>}
    </label>
    <input
      type={type}
      name={name}
      value={value ?? ''}
      onChange={onChange}
      placeholder={placeholder}
      disabled={disabled}
      inputMode={inputMode}
      min={min}
      step={step}
      autoComplete="off"
      data-form-type="other"
      className={`
        w-full px-3 py-2 border rounded-lg text-sm
        focus:outline-none focus:ring-2
        disabled:bg-slate-50 disabled:text-slate-400
        ${error ? 'border-rose-300 focus:border-rose-500 focus:ring-rose-500/20' : 'border-slate-200 focus:border-indigo-500 focus:ring-indigo-500/20'}
        ${disabled ? 'cursor-not-allowed' : ''}
      `}
    />
    {error && <p className="text-xs text-rose-500">{error}</p>}
    {help && <p className="text-xs text-slate-400">{help}</p>}
  </div>
);

const FormCheckbox = ({ label, name, checked, onChange, help, icon: Icon, disabled = false }) => (
  <button
    type="button"
    role="switch"
    aria-checked={!!checked}
    aria-label={label}
    disabled={disabled}
    onClick={() => onChange?.({ target: { name, type: 'checkbox', checked: !checked } })}
    className={`flex w-full items-center justify-between gap-4 rounded-xl border px-4 py-3 text-left transition-colors focus:outline-none focus-visible:ring-2 focus-visible:ring-indigo-500/30 ${
      checked ? 'border-indigo-200 bg-indigo-50/40' : 'border-slate-200 bg-white hover:bg-slate-50/60'
    } ${disabled ? 'cursor-not-allowed opacity-70' : ''}`}
  >
    <span className="flex min-w-0 items-start gap-3">
      {Icon && <span className="mt-0.5 rounded-lg bg-slate-100 p-1.5 text-slate-500"><Icon size={16} /></span>}
      <span className="min-w-0">
        <span className="block text-sm font-medium text-slate-800">{label}</span>
        {help && <span className="mt-1 block text-xs leading-5 text-slate-500">{help}</span>}
      </span>
    </span>
    <span aria-hidden="true" className={`relative inline-flex h-5 w-9 shrink-0 items-center rounded-full transition-colors ${checked ? 'bg-indigo-500' : 'bg-slate-200'}`}>
      <span className={`inline-block h-4 w-4 rounded-full bg-white shadow-sm ring-1 ring-black/5 transition-transform ${checked ? 'translate-x-4' : 'translate-x-0.5'}`} />
    </span>
  </button>
);

const getEndpointCredentialGroupID = (endpoint) => Number(endpoint?.credentialGroupId || endpoint?.credential_group_id || 0);

const getNextEndpointPriority = (existingEndpoints = [], channel = '', credentialGroupId = 0) => {
  const targetChannel = String(channel || '').trim();
  const targetCredentialGroupID = Number(credentialGroupId || 0);
  const maxPriority = (Array.isArray(existingEndpoints) ? existingEndpoints : []).reduce((max, endpoint) => {
    if (!endpoint) return max;
    if (String(endpoint.channel || '').trim() !== targetChannel) return max;
    if (getEndpointCredentialGroupID(endpoint) !== targetCredentialGroupID) return max;
    const priority = Number(endpoint.priority);
    return Number.isFinite(priority) ? Math.max(max, Math.floor(priority)) : max;
  }, -1);
  return maxPriority + 1;
};

// ============================================
// 端点表单组件
// ============================================

const EndpointForm = ({
  endpoint = null,  // null = 新建模式, object = 编辑模式
  channels = [],
  credentialGroupsByChannel = {},
  existingEndpoints = [],
  defaultChannel = '',
  defaultCredentialGroupId = 0,
  lockChannel = false,
  initialSetup = false,
  onSave,
  onCancel,
  loading = false
}) => {
  const isEditMode = !!endpoint;
  const initialCredentialGroupID = Number(endpoint?.credentialGroupId || endpoint?.credential_group_id || defaultCredentialGroupId || 0);
  // 编辑模式下渠道名称不可修改（渠道是端点的核心标识之一）
  const channelDisabled = loading || isEditMode || lockChannel;
  const uniqueChannels = Array.from(new Set((channels || []).filter(Boolean).map(String)));

  // 计算初始表单数据
  const getInitialFormData = () => {
    if (endpoint) {
      return {
        channel: endpoint.channel || '',
        credentialGroupId: endpoint.credentialGroupId || endpoint.credential_group_id || 0,
        name: endpoint.name || '',
        url: endpoint.url || '',
        priority: String(endpoint.priority ?? 0),
        failoverEnabled: endpoint.failoverEnabled !== false,
        skipHealthCheck: endpoint.skipHealthCheck || endpoint.skip_health_check || false,
        cooldownSeconds: endpoint.cooldownSeconds || '',
        timeoutSeconds: endpoint.timeoutSeconds ?? endpoint.timeout_seconds ?? 0,
        supportsCountTokens: endpoint.supportsCountTokens || false,
        // 端点级倍率已停用（对齐 Codex）：倍率唯一来源是凭证分组，
        // app.go 构造 EndpointMultiplier 时不再读取这些字段。
        // 保留原样回传仅为不抹掉 DB 中的历史值，表单不提供编辑入口。
        costMultiplier: endpoint.costMultiplier || 1.0,
        inputCostMultiplier: endpoint.inputCostMultiplier || 1.0,
        outputCostMultiplier: endpoint.outputCostMultiplier || 1.0,
        cacheCreationCostMultiplier: endpoint.cacheCreationCostMultiplier || 1.0,
        cacheCreationCostMultiplier1h: endpoint.cacheCreationCostMultiplier1h || 1.0,
        cacheReadCostMultiplier: endpoint.cacheReadCostMultiplier || 1.0,
        modelMultipliers: endpoint.modelMultipliers || endpoint.model_multipliers || []
      };
    }
    return {
      channel: defaultChannel || '',
      credentialGroupId: Number(defaultCredentialGroupId) || 0,
      name: '',
      url: '',
      priority: '',
      failoverEnabled: true,
      skipHealthCheck: false, // 默认不跳过健康检查
      cooldownSeconds: '',
      timeoutSeconds: 0, // 新建时传0，使用全局默认超时
      supportsCountTokens: false,
      costMultiplier: '1',
      inputCostMultiplier: '1',
      outputCostMultiplier: '1',
      cacheCreationCostMultiplier: '1',
      cacheCreationCostMultiplier1h: '1',
      cacheReadCostMultiplier: '1',
      modelMultipliers: []
    };
  };

  // 表单状态
  const [formData, setFormData] = useState(getInitialFormData);
  const [activeTab, setActiveTab] = useState('basic');

  // 表单错误
  const [errors, setErrors] = useState({});

  const channelOptions = [
    ...((!uniqueChannels.includes(formData.channel) && formData.channel)
      ? [{ value: formData.channel, label: `${formData.channel} (当前)` }]
      : []),
    ...uniqueChannels.map((channel) => ({ value: channel, label: channel }))
  ];
  const selectedChannelGroups = credentialGroupsByChannel?.[formData.channel] || [];
  const selectedCredentialGroupID = Number(formData.credentialGroupId || 0);
  const selectedCredentialGroup = selectedChannelGroups.find((group) => Number(group.id) === selectedCredentialGroupID) ||
    selectedChannelGroups.find((group) => group.isDefault || group.is_default) ||
    selectedChannelGroups[0] ||
    null;
  const subscriptionMode = isSubscriptionGroup(selectedCredentialGroup);
  const credentialGroupOptions = selectedChannelGroups.map((group) => ({
    value: String(group.id),
    label: `${group.name || '未命名分组'}${group.isDefault || group.is_default ? ' · 默认' : ''}`
  }));
  const groupMultiplierItems = [
    ['总', selectedCredentialGroup?.costMultiplier ?? 1],
    ['输入', selectedCredentialGroup?.inputCostMultiplier ?? 1],
    ['输出', selectedCredentialGroup?.outputCostMultiplier ?? 1],
    ['缓存读取', selectedCredentialGroup?.cacheReadCostMultiplier ?? 1]
  ];
  const formatGroupMultiplier = (value) => {
    const parsed = Number(value);
    return `${formatMultiplierExpression(Number.isFinite(parsed) ? parsed : 1, 3)}x`;
  };

  // 处理输入变化
  const handleChange = (e) => {
    const { name, value, type, checked } = e.target;
    setFormData(prev => {
      const next = {
        ...prev,
        [name]: type === 'checkbox' ? checked : value
      };
      if (name === 'channel') {
        const groups = credentialGroupsByChannel?.[value] || [];
        const defaultGroup = groups.find((group) => group.isDefault || group.is_default) || groups[0] || null;
        next.credentialGroupId = defaultGroup?.id || 0;
        next.priority = isEditMode ? String(getNextEndpointPriority(existingEndpoints, value, next.credentialGroupId)) : '';
      }
      return next;
    });
    // 清除对应字段的错误
    if (errors[name]) {
      setErrors(prev => ({ ...prev, [name]: null }));
    }
  };

  // 表单验证
  const validateForm = () => {
    const newErrors = {};

    if (!formData.channel.trim()) {
      newErrors.channel = '请输入渠道名称';
    }
    if (!formData.name.trim()) {
      newErrors.name = '请输入端点名称';
    }
    if (!subscriptionMode && !formData.url.trim()) {
      newErrors.url = '请输入端点 URL';
    } else if (!subscriptionMode && !/^https?:\/\/.+/.test(formData.url)) {
      newErrors.url = '请输入有效的 URL (以 http:// 或 https:// 开头)';
    }
    const priorityRaw = String(formData.priority ?? '').trim();
    if (priorityRaw !== '') {
      const priorityValue = Number(priorityRaw);
      if (!Number.isInteger(priorityValue) || priorityValue < 0) {
        newErrors.priority = '优先级必须是大于等于 0 的整数';
      }
    }
    const timeoutRaw = String(formData.timeoutSeconds ?? '').trim();
    if (timeoutRaw !== '' && (!Number.isInteger(Number(timeoutRaw)) || Number(timeoutRaw) < 0)) {
      newErrors.timeoutSeconds = '超时时间必须是大于等于 0 的整数秒数';
    }

    const targetChannel = formData.channel.trim();
    const targetName = formData.name.trim();
    const targetCredentialGroupID = Number(formData.credentialGroupId || selectedCredentialGroup?.id || 0);
    if (targetChannel && targetName && Array.isArray(existingEndpoints) && existingEndpoints.length > 0) {
      const conflict = existingEndpoints.find((ep) => {
        if (!ep) return false;
        const epChannel = String(ep.channel || '').trim();
        const epName = String(ep.name || '').trim();
        const epCredentialGroupID = Number(ep.credentialGroupId || ep.credential_group_id || 0);
        if (epChannel !== targetChannel || epCredentialGroupID !== targetCredentialGroupID || epName !== targetName) return false;
        if (isEditMode && endpoint?.id && ep.id === endpoint.id) return false;
        return true;
      });

      if (conflict) {
        const groupName = selectedCredentialGroup?.name || '默认分组';
        newErrors.submit = `同一凭证分组内端点名称必须唯一：渠道 '${targetChannel}' / 分组 '${groupName}' 已存在同名端点 '${targetName}'，请修改名称或选择其他分组`;
        newErrors.name = newErrors.submit;
      }
    }

    if (newErrors.channel || newErrors.name || newErrors.url) {
      setActiveTab('basic');
    } else if (newErrors.priority || newErrors.timeoutSeconds) {
      setActiveTab('routing');
    }

    setErrors(newErrors);
    return Object.keys(newErrors).length === 0;
  };

  // 提交表单
  const handleSubmit = async (e) => {
    e.preventDefault();

    if (!validateForm()) {
      return;
    }

    try {
      const endpointPayload = { ...formData };
      if (subscriptionMode) { endpointPayload.url = 'https://api.anthropic.com'; endpointPayload.supportsCountTokens = true; }
      delete endpointPayload.token;
      delete endpointPayload.apiKey;
      if (isEditMode && String(endpointPayload.priority ?? '').trim() === '') {
        endpointPayload.priority = endpoint?.priority ?? 0;
      }
      await onSave({
        ...endpointPayload,
        timeoutSeconds: Number(formData.timeoutSeconds || 0),
        credentialGroupId: Number(formData.credentialGroupId || selectedCredentialGroup?.id || 0) || 0,
        modelMultipliers: formData.modelMultipliers || []
      });
    } catch (error) {
      console.error('保存失败:', error);
      setErrors({ submit: getErrorMessage(error, '保存失败') });
    }
  };

  return renderModalPortal(
    <div className="fixed inset-0 bg-slate-900/30 backdrop-blur-sm flex items-start justify-center z-50 animate-fade-in pt-[15vh]">
      <div className="bg-white rounded-2xl shadow-xl w-full max-w-2xl max-h-[75vh] flex flex-col overflow-hidden">
        {/* 标题栏 */}
        <div className="flex items-center justify-between px-6 py-4 border-b border-slate-100 flex-shrink-0">
          <div>
            <h2 className="text-lg font-semibold text-slate-900">
              {initialSetup ? '添加渠道 · 3/3' : isEditMode ? '编辑端点' : '新建端点'}
            </h2>
            {initialSetup && <p className="mt-0.5 text-xs text-slate-500">配置首个端点，确认后创建渠道</p>}
          </div>
          <button
            type="button"
            onClick={onCancel}
            disabled={loading}
            aria-label="关闭"
            className="p-2 text-slate-400 hover:text-slate-600 hover:bg-slate-100 rounded-lg transition-colors"
          >
            <X size={20} />
          </button>
        </div>

        {/* 表单内容 - 可滚动区域 */}
        <form onSubmit={handleSubmit} className="flex flex-col flex-1 overflow-hidden" autoComplete="off">
          <div className="border-b border-slate-100 bg-slate-50/70 px-6 py-3">
            <SegmentedTabs
              tabs={ENDPOINT_FORM_TABS}
              activeTab={activeTab}
              onChange={setActiveTab}
              disabled={loading}
              className="grid-cols-2"
              buttonPaddingClass="px-2 sm:px-3"
            />
          </div>

          <div className="p-6 overflow-y-auto flex-1">
          {/* 提交错误提示 */}
          {errors.submit && (
            <div className="mb-4 p-3 bg-rose-50 border border-rose-200 rounded-lg flex items-center gap-2 text-rose-700 text-sm">
              <AlertCircle size={16} />
              {errors.submit}
            </div>
          )}

          {activeTab === 'basic' && selectedCredentialGroup && (
            <div className="mb-4 rounded-xl border border-slate-200 bg-slate-50/70 p-3">
              <div className="flex items-center gap-2 text-xs font-medium text-slate-700">
                <KeyRound size={14} />
                归属凭证分组
              </div>
              <div className="mt-2 flex min-w-0 flex-wrap items-center gap-2 text-xs">
                <span className="inline-flex max-w-full items-center gap-1 rounded-md bg-white px-2.5 py-1.5 font-medium text-slate-700">
                  <span className="truncate">{selectedCredentialGroup.name || '默认分组'}</span>
                  {(selectedCredentialGroup.isDefault || selectedCredentialGroup.is_default) && (
                    <span className="rounded bg-emerald-50 px-1 text-[10px] font-semibold text-emerald-600">默认</span>
                  )}
                </span>
                {groupMultiplierItems.map(([label, value]) => (
                  <span key={label} className="inline-flex items-center gap-1 rounded-md bg-white px-2 py-1 font-mono text-[11px] font-semibold text-slate-600">
                    <span className="font-sans text-[10px] font-medium text-slate-400">{label}</span>
                    {formatGroupMultiplier(value)}
                  </span>
                ))}
                <span className="inline-flex min-w-0 items-center gap-1 rounded-md bg-white px-2 py-1 text-[11px] font-medium text-slate-500">
                  <Database size={11} />
                  <span className="truncate">分组倍率</span>
                </span>
              </div>
            </div>
          )}

          {/* 基本信息 */}
          {activeTab === 'basic' && (
          <div className="space-y-4">
            <h3 className="text-sm font-semibold text-slate-500 uppercase tracking-wide">
              基本信息
            </h3>

            <div className="grid grid-cols-2 gap-4">
              <div>
                <div className="space-y-1">
                  <label className="block text-sm font-medium text-slate-700">
                    渠道
                    <span className="text-rose-500 ml-1">*</span>
                  </label>
                  {uniqueChannels.length > 0 ? (
                    <CustomSelect
                        options={channelOptions}
                        value={formData.channel || ''}
                        onChange={(value) => {
                          const groups = credentialGroupsByChannel?.[value] || [];
                          const defaultGroup = groups.find((group) => group.isDefault || group.is_default) || groups[0] || null;
                          const nextCredentialGroupId = defaultGroup?.id || 0;
                          setFormData(prev => ({
                            ...prev,
                            channel: value,
                            credentialGroupId: nextCredentialGroupId,
                            priority: isEditMode ? String(getNextEndpointPriority(existingEndpoints, value, nextCredentialGroupId)) : ''
                          }));
                          if (errors.channel) {
                            setErrors(prev => ({ ...prev, channel: null }));
                          }
                        }}
                        disabled={channelDisabled}
                        size="md"
                        fullWidth
                        placeholder="请选择渠道"
                      />
                  ) : (
                    <input
                      type="text"
                      name="channel"
                      value={formData.channel || ''}
                      onChange={handleChange}
                      placeholder="例如：official / backup"
                      disabled={channelDisabled}
                      autoComplete="off"
                      data-form-type="other"
                      className={`
                        w-full px-3 py-2 border border-slate-200 rounded-lg text-sm
                        focus:outline-none focus:ring-2 focus:ring-indigo-500/20 focus:border-indigo-500
                        disabled:bg-slate-50 disabled:text-slate-400
                        ${channelDisabled ? 'cursor-not-allowed' : ''}
                      `}
                    />
                  )}
                  <p className="text-xs text-slate-400">
                    用于分块展示端点，并作为故障转移的第一层边界
                  </p>
                </div>
                {errors.channel && (
                  <p className="text-xs text-rose-500 mt-1">{errors.channel}</p>
                )}
              </div>

              {selectedChannelGroups.length > 0 && (
                <div>
                  <div className="space-y-1">
                    <label className="block text-sm font-medium text-slate-700">
                      凭证分组
                    </label>
                    <CustomSelect
                      options={credentialGroupOptions}
                      value={String(selectedCredentialGroup?.id || formData.credentialGroupId || '')}
                      onChange={(value) => {
                        const nextCredentialGroupId = Number(value) || 0;
                        setFormData(prev => ({
                          ...prev,
                          credentialGroupId: nextCredentialGroupId,
                          priority: isEditMode
                            ? String(nextCredentialGroupId !== initialCredentialGroupID
                              ? getNextEndpointPriority(existingEndpoints, prev.channel, nextCredentialGroupId)
                              : endpoint?.priority ?? getNextEndpointPriority(existingEndpoints, prev.channel, nextCredentialGroupId))
                            : ''
                        }));
                        if (errors.credentialGroupId) {
                          setErrors(prev => ({ ...prev, credentialGroupId: null }));
                        }
                      }}
                      disabled={loading}
                      size="md"
                      fullWidth
                      placeholder="请选择凭证分组"
                    />
                    <p className="text-xs text-slate-400">
                      {subscriptionMode ? '使用该分组所选的订阅账号或客户端登录凭证，端点不保存订阅 token' : '端点归属到该分组后复用其中的 Token/API Key 与默认倍率'}
                    </p>
                  </div>
                  {errors.credentialGroupId && (
                    <p className="text-xs text-rose-500 mt-1">{errors.credentialGroupId}</p>
                  )}
                </div>
              )}

              <div>
                <FormInput
                  label="端点名称"
                  name="name"
                  value={formData.name}
                  onChange={handleChange}
                  placeholder="e.g. api-primary"
                  required
                  disabled={loading}
                  help={isEditMode ? '唯一标识符（可修改，保存后将同步更新历史记录显示）' : '唯一标识符'}
                />
                {errors.name && (
                  <p className="text-xs text-rose-500 mt-1">{errors.name}</p>
                )}
              </div>

              <div className={selectedChannelGroups.length > 0 ? '' : 'col-span-2'}>
                <FormInput
                  label="URL"
                  name="url"
                  value={subscriptionMode ? 'https://api.anthropic.com' : formData.url}
                  disabled={loading || subscriptionMode}
                  help={subscriptionMode ? '订阅端点使用固定官方出口，健康状态由真实请求验证' : undefined}
                  onChange={handleChange}
                  placeholder="https://api.example.com"
                  required
                />
                {errors.url && (
                  <p className="text-xs text-rose-500 mt-1">{errors.url}</p>
                )}
              </div>
            </div>
          </div>
          )}

          {/* 路由配置 */}
          {activeTab === 'routing' && (
          <div className="space-y-3">
            <section className="rounded-xl border border-slate-200 bg-slate-50/60 p-4">
              <div className="mb-4 flex min-w-0 flex-wrap items-center justify-between gap-2">
                <h3 className="flex items-center gap-2 text-sm font-semibold text-slate-800"><Route size={15} className="text-slate-500" />请求路由</h3>
                <span className="max-w-full truncate rounded-md border border-slate-200 bg-white px-2 py-1 text-xs text-slate-500">{formData.channel} · {selectedCredentialGroup?.name || '默认分组'}</span>
              </div>
              <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
                <div>
                  <FormInput
                    label="优先级"
                    name="priority"
                    value={formData.priority}
                    onChange={handleChange}
                    type="number"
                    min="0"
                    disabled={loading}
                    placeholder={`留空自动分配 P${getNextEndpointPriority(existingEndpoints, formData.channel, selectedCredentialGroupID)}`}
                    help={isEditMode ? '数字越小越优先；同分组优先级相同则交换。' : `留空分配 P${getNextEndpointPriority(existingEndpoints, formData.channel, selectedCredentialGroupID)}；数字越小越优先。`}
                  />
                  {errors.priority && (
                    <p className="text-xs text-rose-500 mt-1">{errors.priority}</p>
                  )}
                </div>

                <FormInput
                  label="非流式超时（秒）"
                  name="timeoutSeconds"
                  value={formData.timeoutSeconds || ''}
                  onChange={handleChange}
                  type="number"
                  min="0"
                  step="1"
                  disabled={loading}
                  error={errors.timeoutSeconds}
                  placeholder="跟随全局设置"
                  help="留空或 0 跟随全局，正整数覆盖此端点。"
                />
              </div>
              <p className="mt-3 flex items-start gap-2 border-t border-slate-200/70 pt-3 text-xs leading-5 text-slate-500"><Clock3 size={13} className="mt-1 shrink-0" />此处限制非流式请求的总等待时间；流式响应头超时在「设置 → 请求控制」中调整。</p>
            </section>

            <div className="space-y-3">
              <FormCheckbox
                label="跳过健康检查"
                name="skipHealthCheck"
                checked={formData.skipHealthCheck}
                onChange={handleChange}
                disabled={loading || subscriptionMode}
                icon={ShieldOff}
                help={subscriptionMode ? '官方订阅不发送定时探测，健康状态由真实请求验证。' : '开启后跳过定时探测；关闭时仍需全局开启，且分组被当前调度方案选用。'}
              />

              <FormCheckbox
                label="支持 count_tokens"
                name="supportsCountTokens"
                checked={formData.supportsCountTokens}
                onChange={handleChange}
                disabled={loading}
                icon={Hash}
                help="端点是否支持 Token 计数 API"
              />
            </div>
          </div>
          )}

          </div>

          {/* 按钮 - 固定在底部 */}
          <div className="flex justify-end gap-3 px-6 py-4 border-t border-slate-100 bg-white flex-shrink-0">
            <Button
              type="button"
              variant="ghost"
              onClick={onCancel}
              disabled={loading}
            >
              取消
            </Button>
            <Button
              type="submit"
              icon={Save}
              loading={loading}
            >
              {initialSetup ? '完成创建' : isEditMode ? '保存修改' : '创建端点'}
            </Button>
          </div>
        </form>
      </div>
    </div>
  );
};

export default EndpointForm;
