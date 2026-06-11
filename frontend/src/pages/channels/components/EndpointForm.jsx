// ============================================
// 端点编辑表单组件 (SQLite 存储模式)
// (2026-01-10) - 新建时隐藏优先级和超时字段（自动递增/使用全局默认）
// ============================================

import { useState } from 'react';
import { X, Save, AlertCircle, ChevronDown, Eye, EyeOff, Plus, Trash2 } from 'lucide-react';
import { Button, CustomSelect, SegmentedTabs, Tooltip } from '@components/ui';
import { renderModalPortal } from '@components/ui/modalPortal.js';
import { formatMultiplierInputValue, isValidMultiplierExpression } from '@utils/multiplier.js';

const MULTIPLIER_EXPRESSION_HELP = '支持小数或分数，如 1.25、40/30';
const MULTIPLIER_FIELDS = [
  { name: 'costMultiplier', label: '总成本倍率' },
  { name: 'inputCostMultiplier', label: '输入成本倍率' },
  { name: 'outputCostMultiplier', label: '输出成本倍率' },
  { name: 'cacheReadCostMultiplier', label: '缓存读取成本倍率' },
  { name: 'cacheCreationCostMultiplier', label: '5分钟缓存创建倍率' },
  { name: 'cacheCreationCostMultiplier1h', label: '1小时缓存创建倍率' }
];

const MODEL_MULTIPLIER_FIELDS = [
  { name: 'costMultiplier', label: '总倍率' },
  { name: 'inputCostMultiplier', label: '输入倍率' },
  { name: 'outputCostMultiplier', label: '输出倍率' },
  { name: 'cacheReadCostMultiplier', label: '缓存读取倍率' }
];

const ENDPOINT_FORM_TABS = [
  { key: 'basic', label: '基础信息' },
  { key: 'auth', label: '认证信息' },
  { key: 'routing', label: '路由配置' },
  { key: 'billing', label: '成本倍率' }
];

const hasFineGrainedModelMultiplier = (rule) => {
  const isDefault = (value) => {
    const parsed = Number(value ?? 1.0);
    return Number.isFinite(parsed) && Math.abs(parsed - 1.0) <= 0.000001;
  };
  return !isDefault(rule?.inputCostMultiplier) ||
    !isDefault(rule?.outputCostMultiplier) ||
    !isDefault(rule?.cacheReadCostMultiplier);
};

const buildInitialModelMultiplierRows = (modelMultipliers) => {
  return (modelMultipliers || [])
    .map((rule) => ({
      model: String(rule?.model || ''),
      costMultiplier: formatMultiplierInputValue(rule?.costMultiplier ?? 1.0),
      inputCostMultiplier: formatMultiplierInputValue(rule?.inputCostMultiplier ?? 1.0),
      outputCostMultiplier: formatMultiplierInputValue(rule?.outputCostMultiplier ?? 1.0),
      cacheReadCostMultiplier: formatMultiplierInputValue(rule?.cacheReadCostMultiplier ?? 1.0),
      advancedOpen: hasFineGrainedModelMultiplier(rule)
    }))
    .filter((row) => row.model.trim());
};

const createEmptyModelMultiplierRow = () => ({
  model: '',
  costMultiplier: '1',
  inputCostMultiplier: '1',
  outputCostMultiplier: '1',
  cacheReadCostMultiplier: '1',
  advancedOpen: false
});

const parseModelMultiplierPayload = (rows) => {
  const payload = [];
  const seen = new Set();
  for (const [index, row] of (rows || []).entries()) {
    const model = String(row?.model || '').trim();
    if (!model) continue;
    if (seen.has(model)) {
      return { error: `模型附加倍率重复：${model}` };
    }
    seen.add(model);

    for (const field of MODEL_MULTIPLIER_FIELDS) {
      if (!isValidMultiplierExpression(row?.[field.name])) {
        return { error: `第 ${index + 1} 行模型附加倍率：${field.label}必须是非负数字或分数，例如 1.25 或 40/30` };
      }
    }

    payload.push({
      model,
      costMultiplier: row.costMultiplier,
      inputCostMultiplier: row.inputCostMultiplier,
      outputCostMultiplier: row.outputCostMultiplier,
      cacheReadCostMultiplier: row.cacheReadCostMultiplier
    });
  }
  return { payload };
};

const getErrorMessage = (error, fallback = '保存失败') => {
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

// ============================================
// 表单输入组件
// ============================================

const FormInput = ({ label, name, value, onChange, type = 'text', placeholder, required, disabled, help, error, inputMode }) => (
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

// 密码输入组件（带显示/隐藏切换）
// 修复浏览器原生按钮与自定义按钮重复问题
const PasswordInput = ({ label, name, value, onChange, placeholder, required, help }) => {
  const [showPassword, setShowPassword] = useState(false);

  return (
    <div className="space-y-1">
      <label className="block text-sm font-medium text-slate-700">
        {label}
        {required && <span className="text-rose-500 ml-1">*</span>}
      </label>
      <div className="relative">
        <input
          type={showPassword ? 'text' : 'password'}
          name={name}
          value={value || ''}
          onChange={onChange}
          placeholder={placeholder}
          autoComplete="new-password"
          data-form-type="other"
          className="w-full px-3 py-2 pr-10 border border-slate-200 rounded-lg text-sm focus:outline-none focus:ring-2 focus:ring-indigo-500/20 focus:border-indigo-500 [&::-ms-reveal]:hidden [&::-ms-clear]:hidden [&::-webkit-credentials-auto-fill-button]:hidden"
        />
        <Tooltip content={showPassword ? '隐藏' : '显示'} className="absolute right-2 top-1/2 -translate-y-1/2">
          <button
            type="button"
            onClick={() => setShowPassword(!showPassword)}
            className="p-1 text-slate-400 hover:text-slate-600 transition-colors"
            aria-label={showPassword ? '隐藏' : '显示'}
          >
            {showPassword ? <EyeOff size={18} /> : <Eye size={18} />}
          </button>
        </Tooltip>
      </div>
      {help && <p className="text-xs text-slate-400">{help}</p>}
    </div>
  );
};

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

// ============================================
// 端点表单组件
// ============================================

const EndpointForm = ({
  endpoint = null,  // null = 新建模式, object = 编辑模式
  channels = [],
  existingEndpoints = [],
  defaultChannel = '',
  lockChannel = false,
  onSave,
  onCancel,
  loading = false
}) => {
  const isEditMode = !!endpoint;
  // 编辑模式下渠道名称不可修改（渠道是端点的核心标识之一）
  const channelDisabled = loading || isEditMode || lockChannel;
  const uniqueChannels = Array.from(new Set((channels || []).filter(Boolean).map(String)));

  // 计算初始表单数据
  const getInitialFormData = () => {
    if (endpoint) {
      return {
        channel: endpoint.channel || '',
        name: endpoint.name || '',
        url: endpoint.url || '',
        token: endpoint.token || '', // 本地桌面应用，直接显示已保存的 Token
        apiKey: endpoint.apiKey || '', // 本地桌面应用，直接显示已保存的 ApiKey
        priority: endpoint.priority || 1,
        failoverEnabled: endpoint.failoverEnabled !== false,
        skipHealthCheck: endpoint.skipHealthCheck || endpoint.skip_health_check || false,
        cooldownSeconds: endpoint.cooldownSeconds || '',
        timeoutSeconds: endpoint.timeoutSeconds && endpoint.timeoutSeconds !== 300 ? endpoint.timeoutSeconds : '', // 300 是全局默认，显示为空
        supportsCountTokens: endpoint.supportsCountTokens || false,
        costMultiplier: formatMultiplierInputValue(endpoint.costMultiplier || 1.0),
        inputCostMultiplier: formatMultiplierInputValue(endpoint.inputCostMultiplier || 1.0),
        outputCostMultiplier: formatMultiplierInputValue(endpoint.outputCostMultiplier || 1.0),
        cacheCreationCostMultiplier: formatMultiplierInputValue(endpoint.cacheCreationCostMultiplier || 1.0),
        cacheCreationCostMultiplier1h: formatMultiplierInputValue(endpoint.cacheCreationCostMultiplier1h || 1.0),
        cacheReadCostMultiplier: formatMultiplierInputValue(endpoint.cacheReadCostMultiplier || 1.0)
      };
    }
    return {
      channel: defaultChannel || '',
      name: '',
      url: '',
      token: '',
      apiKey: '',
      priority: 0, // 新建时传0，让后端自动递增
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
      cacheReadCostMultiplier: '1'
    };
  };

  // 表单状态
  const [formData, setFormData] = useState(getInitialFormData);
  const [modelMultiplierRows, setModelMultiplierRows] = useState(() => buildInitialModelMultiplierRows(endpoint?.modelMultipliers));
  const [activeTab, setActiveTab] = useState('basic');

  // 表单错误
  const [errors, setErrors] = useState({});

  const channelOptions = [
    ...((!uniqueChannels.includes(formData.channel) && formData.channel)
      ? [{ value: formData.channel, label: `${formData.channel} (当前)` }]
      : []),
    ...uniqueChannels.map((channel) => ({ value: channel, label: channel }))
  ];

  // 处理输入变化
  const handleChange = (e) => {
    const { name, value, type, checked } = e.target;
    setFormData(prev => ({
      ...prev,
      [name]: type === 'checkbox' ? checked : value
    }));
    // 清除对应字段的错误
    if (errors[name]) {
      setErrors(prev => ({ ...prev, [name]: null }));
    }
  };

  const updateModelMultiplierRow = (index, key, value) => {
    setModelMultiplierRows((prev) =>
      (prev || []).map((row, rowIndex) =>
        rowIndex === index ? { ...row, [key]: value } : row
      )
    );
    if (errors.submit) {
      setErrors((prev) => ({ ...prev, submit: null }));
    }
  };

  const addModelMultiplierRow = () => {
    setModelMultiplierRows((prev) => [...(prev || []), createEmptyModelMultiplierRow()]);
  };

  const removeModelMultiplierRow = (index) => {
    setModelMultiplierRows((prev) => (prev || []).filter((_, rowIndex) => rowIndex !== index));
  };

  const toggleModelMultiplierDetails = (index) => {
    setModelMultiplierRows((prev) =>
      (prev || []).map((row, rowIndex) =>
        rowIndex === index ? { ...row, advancedOpen: !row.advancedOpen } : row
      )
    );
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
    if (!isEditMode && !formData.token.trim()) {
      newErrors.token = '请输入 Token';
    }
    for (const field of MULTIPLIER_FIELDS) {
      if (!isValidMultiplierExpression(formData[field.name])) {
        const message = `${field.label}必须是非负数字或分数，例如 1.25 或 40/30`;
        newErrors[field.name] = message;
        if (!newErrors.submit) {
          newErrors.submit = message;
        }
      }
    }
    const modelMultiplierResult = parseModelMultiplierPayload(modelMultiplierRows);
    if (modelMultiplierResult.error) {
      newErrors.submit = modelMultiplierResult.error;
      newErrors.modelMultipliers = modelMultiplierResult.error;
    }

    const targetChannel = formData.channel.trim();
    const targetName = formData.name.trim();
    if (targetChannel && targetName && Array.isArray(existingEndpoints) && existingEndpoints.length > 0) {
      const conflict = existingEndpoints.find((ep) => {
        if (!ep) return false;
        const epChannel = String(ep.channel || '').trim();
        const epName = String(ep.name || '').trim();
        if (epChannel !== targetChannel || epName !== targetName) return false;
        if (isEditMode && endpoint?.id && ep.id === endpoint.id) return false;
        return true;
      });

      if (conflict) {
        newErrors.submit = `同一渠道内端点名称必须唯一：渠道 '${targetChannel}' 已存在同名端点 '${targetName}'，请修改名称或选择其他渠道`;
        newErrors.name = newErrors.submit;
      }
    }

    if (newErrors.channel || newErrors.name || newErrors.url) {
      setActiveTab('basic');
    } else if (newErrors.token) {
      setActiveTab('auth');
    } else if (MULTIPLIER_FIELDS.some((field) => newErrors[field.name]) || newErrors.modelMultipliers) {
      setActiveTab('billing');
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
      const modelMultiplierResult = parseModelMultiplierPayload(modelMultiplierRows);
      if (modelMultiplierResult.error) {
        setActiveTab('billing');
        setErrors({ submit: modelMultiplierResult.error });
        return;
      }
      await onSave({
        ...formData,
        modelMultipliers: modelMultiplierResult.payload
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
              className="grid-cols-4"
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
                          setFormData(prev => ({ ...prev, channel: value }));
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
            </div>

            <div>
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
          )}

          {/* 认证信息 */}
          {activeTab === 'auth' && (
          <div className="space-y-4">
            <h3 className="text-sm font-semibold text-slate-500 uppercase tracking-wide">
              认证信息
            </h3>

            <div>
              <PasswordInput
                label="Token"
                name="token"
                value={formData.token}
                onChange={handleChange}
                placeholder="sk-..."
                required={!isEditMode}
                help="Bearer Token 认证。清空后保存将保留原值"
              />
              {errors.token && (
                <p className="text-xs text-rose-500 mt-1">{errors.token}</p>
              )}
            </div>

            <div>
              <PasswordInput
                label="API Key (可选)"
                name="apiKey"
                value={formData.apiKey}
                onChange={handleChange}
                placeholder="可选的 API Key"
                help="备用认证方式。清空后保存将保留原值"
              />
            </div>
          </div>
          )}

          {/* 路由配置（仅编辑模式显示标题和输入项） */}
          {activeTab === 'routing' && (
          <div className="space-y-4">
            {isEditMode && (
              <h3 className="text-sm font-semibold text-slate-500 uppercase tracking-wide">
                路由配置
              </h3>
            )}

            {isEditMode && (
              <div className="grid gap-4 grid-cols-2">
                <FormInput
                  label="优先级"
                  name="priority"
                  value={formData.priority}
                  onChange={handleChange}
                  type="number"
                  placeholder="1"
                  help="数字越小优先级越高，相同则交换"
                />

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
            )}

            <div className="grid grid-cols-1 sm:grid-cols-3 gap-3">
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
            </div>
          </div>
          )}

          {/* 成本倍率 */}
          {activeTab === 'billing' && (
              <div className="rounded-xl bg-slate-50 p-4 space-y-4">
                <p className="text-xs text-slate-500 mb-3">
                  成本倍率用于调整不同端点的计费比例，默认为 1.0，{MULTIPLIER_EXPRESSION_HELP}
                </p>

                <div className="grid grid-cols-2 gap-4">
                  <FormInput
                    label="总成本倍率"
                    name="costMultiplier"
                    value={formData.costMultiplier}
                    onChange={handleChange}
                    type="text"
                    inputMode="decimal"
                    placeholder="1.0"
                    error={errors.costMultiplier}
                  />

                  <FormInput
                    label="输入成本倍率"
                    name="inputCostMultiplier"
                    value={formData.inputCostMultiplier}
                    onChange={handleChange}
                    type="text"
                    inputMode="decimal"
                    placeholder="1.0"
                    error={errors.inputCostMultiplier}
                  />

                  <FormInput
                    label="输出成本倍率"
                    name="outputCostMultiplier"
                    value={formData.outputCostMultiplier}
                    onChange={handleChange}
                    type="text"
                    inputMode="decimal"
                    placeholder="40/30"
                    error={errors.outputCostMultiplier}
                  />

                  <FormInput
                    label="缓存读取成本倍率"
                    name="cacheReadCostMultiplier"
                    value={formData.cacheReadCostMultiplier}
                    onChange={handleChange}
                    type="text"
                    inputMode="decimal"
                    placeholder="1.0"
                    error={errors.cacheReadCostMultiplier}
                  />

                  <FormInput
                    label="5分钟缓存创建倍率"
                    name="cacheCreationCostMultiplier"
                    value={formData.cacheCreationCostMultiplier}
                    onChange={handleChange}
                    type="text"
                    inputMode="decimal"
                    placeholder="1.0"
                    error={errors.cacheCreationCostMultiplier}
                    help="Claude 默认缓存类型"
                  />

                  <FormInput
                    label="1小时缓存创建倍率"
                    name="cacheCreationCostMultiplier1h"
                    value={formData.cacheCreationCostMultiplier1h}
                    onChange={handleChange}
                    type="text"
                    inputMode="decimal"
                    placeholder="1.0"
                    error={errors.cacheCreationCostMultiplier1h}
                    help="长效缓存类型"
                  />
                </div>

                <div className="rounded-xl border border-slate-200 bg-white p-3">
                  <div className="flex items-center justify-between gap-3">
                    <div>
                      <label className="block text-sm font-medium text-slate-700">模型附加倍率</label>
                      <p className="mt-0.5 text-xs text-slate-400">请求使用映射后的模型命中规则；默认只设置总倍率。</p>
                    </div>
                    <button
                      type="button"
                      onClick={addModelMultiplierRow}
                      className="inline-flex items-center gap-1.5 rounded-lg border border-slate-200 bg-white px-2.5 py-1.5 text-xs font-medium text-slate-600 transition-colors hover:border-indigo-200 hover:bg-indigo-50 hover:text-indigo-700"
                    >
                      <Plus size={14} />
                      添加模型
                    </button>
                  </div>

                  {modelMultiplierRows.length > 0 && (
                    <div className="mt-3 space-y-2">
                      {modelMultiplierRows.map((row, index) => (
                        <div key={`model-multiplier-${index}`} className="rounded-lg border border-slate-200 bg-slate-50/70 p-3">
                          <div className="grid grid-cols-1 gap-2 sm:grid-cols-[minmax(0,1fr)_8rem_auto_auto] sm:items-end">
                            <div className="min-w-0">
                              <label className="mb-1 block text-xs text-slate-500">模型</label>
                              <input
                                type="text"
                                value={row.model}
                                onChange={(event) => updateModelMultiplierRow(index, 'model', event.target.value)}
                                className="w-full rounded-lg border border-slate-200 px-2.5 py-1.5 font-mono text-sm focus:border-indigo-500 focus:outline-none focus:ring-2 focus:ring-indigo-500"
                                placeholder="claude-sonnet-4-20250514"
                              />
                            </div>

                            <div>
                              <label className="mb-1 block text-xs text-slate-500">总倍率</label>
                              <input
                                type="text"
                                inputMode="decimal"
                                value={row.costMultiplier}
                                onChange={(event) => updateModelMultiplierRow(index, 'costMultiplier', event.target.value)}
                                className="w-full rounded-lg border border-slate-200 px-2.5 py-1.5 text-sm focus:border-indigo-500 focus:outline-none focus:ring-2 focus:ring-indigo-500"
                                placeholder="1"
                              />
                            </div>

                            <button
                              type="button"
                              onClick={() => toggleModelMultiplierDetails(index)}
                              className="inline-flex h-8 items-center justify-center gap-1.5 rounded-lg border border-slate-200 bg-white px-2.5 text-xs font-medium text-slate-600 transition-colors hover:border-indigo-200 hover:bg-indigo-50 hover:text-indigo-700"
                            >
                              细分
                              <ChevronDown
                                size={14}
                                className={`transition-transform duration-200 ${row.advancedOpen ? 'rotate-180' : ''}`}
                              />
                            </button>

                            <Tooltip content="删除">
                              <button
                                type="button"
                                onClick={() => removeModelMultiplierRow(index)}
                                className="inline-flex h-8 items-center justify-center rounded-lg bg-white p-2 text-slate-400 transition-colors hover:bg-rose-50 hover:text-rose-600"
                              >
                                <Trash2 size={16} />
                              </button>
                            </Tooltip>
                          </div>

                          {row.advancedOpen && (
                            <div className="mt-2 grid grid-cols-1 gap-2 border-t border-slate-200/70 pt-2 sm:grid-cols-3">
                              <div>
                                <label className="mb-1 block text-xs text-slate-500">输入</label>
                                <input
                                  type="text"
                                  inputMode="decimal"
                                  value={row.inputCostMultiplier}
                                  onChange={(event) => updateModelMultiplierRow(index, 'inputCostMultiplier', event.target.value)}
                                  className="w-full rounded-lg border border-slate-200 px-2.5 py-1.5 text-sm focus:border-indigo-500 focus:outline-none focus:ring-2 focus:ring-indigo-500"
                                  placeholder="1"
                                />
                              </div>

                              <div>
                                <label className="mb-1 block text-xs text-slate-500">输出</label>
                                <input
                                  type="text"
                                  inputMode="decimal"
                                  value={row.outputCostMultiplier}
                                  onChange={(event) => updateModelMultiplierRow(index, 'outputCostMultiplier', event.target.value)}
                                  className="w-full rounded-lg border border-slate-200 px-2.5 py-1.5 text-sm focus:border-indigo-500 focus:outline-none focus:ring-2 focus:ring-indigo-500"
                                  placeholder="1"
                                />
                              </div>

                              <div>
                                <label className="mb-1 block text-xs text-slate-500">缓存读取</label>
                                <input
                                  type="text"
                                  inputMode="decimal"
                                  value={row.cacheReadCostMultiplier}
                                  onChange={(event) => updateModelMultiplierRow(index, 'cacheReadCostMultiplier', event.target.value)}
                                  className="w-full rounded-lg border border-slate-200 px-2.5 py-1.5 text-sm focus:border-indigo-500 focus:outline-none focus:ring-2 focus:ring-indigo-500"
                                  placeholder="1"
                                />
                              </div>
                            </div>
                          )}
                        </div>
                      ))}
                    </div>
                  )}
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
