// ============================================
// 端点编辑表单组件 (SQLite 存储模式)
// (2026-01-10) - 新建时后端自动分配优先级，超时使用全局默认
// ============================================

import { useState } from 'react';
import { Database, KeyRound, X, Save, AlertCircle } from 'lucide-react';
import { Button, CustomSelect, SegmentedTabs, Tooltip } from '@components/ui';
import { renderModalPortal } from '@components/ui/modalPortal.js';
import { getErrorMessage } from '@pages/management/shared/formatters.js';
import { formatMultiplierExpression } from '@utils/multiplier.js';

const ENDPOINT_FORM_TABS = [
  { key: 'basic', label: '基础信息' },
  { key: 'routing', label: '路由配置' }
];

// ============================================
// 表单输入组件
// ============================================

const FormInput = ({ label, name, value, onChange, type = 'text', placeholder, required, disabled, help, error, inputMode, min }) => (
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

const FormCheckbox = ({ label, name, checked, onChange, help, disabled = false, compact = false }) => {
  const isChecked = !!checked;
  const toggle = () => {
    if (disabled) return;
    onChange?.({ target: { name, type: 'checkbox', checked: !isChecked } });
  };

  const sizeClasses = compact
    ? {
        wrapper: 'items-center p-2 rounded-lg',
        label: 'text-xs font-semibold text-slate-800',
        help: help ? <p className="text-[10px] text-slate-400 mt-0.5 leading-snug sm:hidden">{help}</p> : null,
        switch: 'h-5 w-9',
        knob: 'h-4 w-4',
        knobTranslate: isChecked ? 'translate-x-4' : 'translate-x-0.5',
      }
    : {
        wrapper: 'items-start p-3 rounded-xl',
        label: 'text-sm font-medium text-slate-800',
        help: help ? <p className="text-xs text-slate-400 mt-1">{help}</p> : null,
        switch: 'h-6 w-11',
        knob: 'h-5 w-5',
        knobTranslate: isChecked ? 'translate-x-5' : 'translate-x-0.5',
      };

  const field = (
    <div
      className={`
        flex justify-between gap-3 border transition-colors ${sizeClasses.wrapper}
        ${disabled ? 'bg-slate-50 opacity-70' : 'bg-white hover:bg-slate-50/60'}
        ${isChecked ? 'border-emerald-200' : 'border-slate-200'}
      `}
      onClick={toggle}
      role="button"
      tabIndex={disabled ? -1 : 0}
      onKeyDown={(e) => {
        if (disabled) return;
        if (e.key === 'Enter' || e.key === ' ') {
          e.preventDefault();
          toggle();
        }
      }}
    >
      <div className="min-w-0">
        <div className={sizeClasses.label}>{label}</div>
        {sizeClasses.help}
      </div>

      <button
        type="button"
        role="switch"
        aria-checked={isChecked}
        aria-label={label}
        disabled={disabled}
        onClick={(e) => {
          e.stopPropagation();
          toggle();
        }}
        className={`
          relative inline-flex ${sizeClasses.switch} flex-shrink-0 items-center rounded-full border transition-colors
          focus:outline-none focus:ring-2 focus:ring-indigo-500/30
          ${disabled ? 'cursor-not-allowed' : 'cursor-pointer'}
          ${isChecked ? 'bg-emerald-500 border-emerald-500' : 'bg-slate-200 border-slate-200'}
        `}
      >
        <span
          className={`
            inline-block ${sizeClasses.knob} rounded-full bg-white shadow-sm ring-1 ring-black/5 transition-transform
            ${sizeClasses.knobTranslate}
          `}
        />
      </button>
    </div>
  );

  if (!compact || !help) return field;

  return (
    <Tooltip content={help} className="block">
      {field}
    </Tooltip>
  );
};

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
        timeoutSeconds: endpoint.timeoutSeconds && endpoint.timeoutSeconds !== 300 ? endpoint.timeoutSeconds : '', // 300 是全局默认，显示为空
        supportsCountTokens: endpoint.supportsCountTokens || false,
        streamOnly: endpoint.streamOnly || endpoint.stream_only || false,
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
      streamOnly: false, // 默认允许非流式请求路由到此端点
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
    if (!formData.url.trim()) {
      newErrors.url = '请输入端点 URL';
    } else if (!/^https?:\/\/.+/.test(formData.url)) {
      newErrors.url = '请输入有效的 URL (以 http:// 或 https:// 开头)';
    }
    const priorityRaw = String(formData.priority ?? '').trim();
    if (priorityRaw !== '') {
      const priorityValue = Number(priorityRaw);
      if (!Number.isInteger(priorityValue) || priorityValue < 0) {
        newErrors.priority = '优先级必须是大于等于 0 的整数';
      }
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
    } else if (newErrors.priority) {
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
      delete endpointPayload.token;
      delete endpointPayload.apiKey;
      if (isEditMode && String(endpointPayload.priority ?? '').trim() === '') {
        endpointPayload.priority = endpoint?.priority ?? 0;
      }
      await onSave({
        ...endpointPayload,
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
          <h2 className="text-lg font-semibold text-slate-900">
            {isEditMode ? '编辑端点' : '新建端点'}
          </h2>
          <button
            onClick={onCancel}
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

          {selectedCredentialGroup && (
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
                      端点归属到该分组后复用其中的 Token/API Key 与默认倍率
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
                  value={formData.url}
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
          <div className="space-y-4">
            <h3 className="text-sm font-semibold text-slate-500 uppercase tracking-wide">
              路由配置
            </h3>

            <div className="grid gap-4 grid-cols-2">
              <div>
                <FormInput
                  label="优先级"
                  name="priority"
                  value={formData.priority}
                  onChange={handleChange}
                  type="number"
                  min="0"
                  placeholder={`留空自动分配 P${getNextEndpointPriority(existingEndpoints, formData.channel, selectedCredentialGroupID)}`}
                  help={isEditMode ? '0 = 最高优先级，数字越小优先级越高，同分组相同则交换' : `留空自动分配为 P${getNextEndpointPriority(existingEndpoints, formData.channel, selectedCredentialGroupID)}；填写后按填写值保存，重复则替换`}
                />
                {errors.priority && (
                  <p className="text-xs text-rose-500 mt-1">{errors.priority}</p>
                )}
              </div>

              {/* 超时时间 - 留空使用全局默认值 */}
              <FormInput
                label="超时时间 (秒)"
                name="timeoutSeconds"
                value={formData.timeoutSeconds || ''}
                onChange={handleChange}
                type="number"
                placeholder="默认: 300 (全局配置)"
                help="单次请求的最大等待时间，留空使用全局配置"
              />
            </div>

            <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
              <FormCheckbox
                label="参与渠道内故障转移"
                name="failoverEnabled"
                checked={formData.failoverEnabled}
                onChange={handleChange}
                disabled={loading}
                compact
                help="关闭后该端点不会参与同一渠道内的端点故障转移"
              />

              <FormCheckbox
                label="跳过健康检查"
                name="skipHealthCheck"
                checked={formData.skipHealthCheck}
                onChange={handleChange}
                disabled={loading}
                compact
                help="部分中转商不支持健康检查端点，勾选后将跳过定时健康检测"
              />

              <FormCheckbox
                label="支持 count_tokens"
                name="supportsCountTokens"
                checked={formData.supportsCountTokens}
                onChange={handleChange}
                disabled={loading}
                compact
                help="端点是否支持 Token 计数 API"
              />

              <FormCheckbox
                label="仅支持流式"
                name="streamOnly"
                checked={formData.streamOnly}
                onChange={handleChange}
                disabled={loading}
                compact
                help="部分中转商仅支持 SSE 流式，勾选后非流式请求不会路由到此端点"
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
              {isEditMode ? '保存修改' : '创建端点'}
            </Button>
          </div>
        </form>
      </div>
    </div>
  );
};

export default EndpointForm;
