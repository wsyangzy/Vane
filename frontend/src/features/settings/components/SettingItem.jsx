// ============================================
// SettingItem - 可编辑的设置项组件
// (2026-01-10) - 循环重试使用双选弹窗
// (2026-01-11) - 支持跳过健康检测开关
// ============================================

import { useState, useEffect } from 'react';
import { AlertTriangle, Eye, EyeOff, RotateCcw, RefreshCw, XCircle, ChevronRight, Timer } from 'lucide-react';
import { CustomSelect, Tooltip } from '../../../shared/ui/index.js';
import LoopRetrySelector from './LoopRetrySelector.jsx';
import DurationInput from './DurationInput.jsx';

// 值类型到输入类型的映射
const VALUE_TYPE_CONFIG = {
  string: { type: 'text', step: null },
  int: { type: 'number', step: 1 },
  float: { type: 'number', step: 0.1 },
  bool: { type: 'checkbox', step: null },
  duration: { type: 'text', step: null, placeholder: '例如: 30s, 5m, 1h' },
  password: { type: 'password', step: null }
};

// 策略类型选项
const STRATEGY_OPTIONS = [
  { value: 'priority', label: 'priority (优先级)' },
  { value: 'fastest', label: 'fastest (最快响应)' }
];

// 强制渲染为下拉框的字段映射（不依赖后端 value_type 是否正确传递）
// key 格式: "category.key"
const FORCED_ENUM_MAP = {
  'logging.level': [
    { value: 'debug', label: '调试 (Debug)' },
    { value: 'info', label: '信息 (Info)' },
    { value: 'warn', label: '警告 (Warn)' },
    { value: 'error', label: '错误 (Error)' }
  ],
  'logging.format': [
    { value: 'text', label: '文本格式' },
    { value: 'json', label: 'JSON 格式' }
  ]
}

const ChangeDot = () => (
  <span className="w-2 h-2 bg-amber-400 rounded-full animate-pulse" />
);

const RevertButton = ({ onClick, disabled }) => (
  <Tooltip content="撤销修改">
    <button
      onClick={onClick}
      disabled={disabled}
      className="p-1.5 text-slate-400 hover:text-indigo-600 hover:bg-indigo-50 rounded-md transition-colors opacity-0 group-hover:opacity-100"
      aria-label="撤销修改"
    >
      <RotateCcw size={14} />
    </button>
  </Tooltip>
);

const SettingItem = ({
  setting,
  value,
  onChange,
  disabled = false,
  hasChange = false,
  onRevert = null,
  // 跳过健康检测相关 props（仅 loop_retry 设置项使用）
  skipHealthCheck = false,
  onSkipHealthCheckChange = null
}) => {
  const [localValue, setLocalValue] = useState(value);
  const [showSecret, setShowSecret] = useState(false);
  const [showLoopRetrySelector, setShowLoopRetrySelector] = useState(false);

  // 同步外部 value 变化
  useEffect(() => {
    setLocalValue(value);
  }, [value]);

  // 检测值类型（优先使用 setting.value_type）
  // CRITICAL: 必须严格检查 value_type 是否存在且非空
  const valueType = (setting.value_type && setting.value_type.trim() !== '')
    ? setting.value_type
    : ((value === 'true' || value === 'false' || value === true || value === false) ? 'bool' : 'string');

  const config = VALUE_TYPE_CONFIG[valueType] || VALUE_TYPE_CONFIG.string;

  // 显示标签（优先使用 label，没有则使用 key）
  const displayLabel = setting.label || setting.key;

  // 处理值变更
  const handleChange = (e) => {
    let newValue;
    if (valueType === 'bool') {
      newValue = e.target.checked ? 'true' : 'false';
    } else {
      newValue = e.target.value;
    }
    setLocalValue(newValue);
    onChange(setting.category, setting.key, newValue);
  };

  // loop_retry 使用弹窗选择器
  if (setting.category === 'failover' && setting.key === 'loop_retry') {
    const isLoopRetry = localValue === 'true' || localValue === true;
    const currentLabel = isLoopRetry ? '循环重试' : '快速失败';
    const CurrentIcon = isLoopRetry ? RefreshCw : XCircle;

    // 处理弹窗选择，同时更新 loop_retry 和 skip_health_check
    const handleLoopRetrySelect = (newValue, newSkipHealthCheck) => {
      setLocalValue(newValue);
      onChange(setting.category, setting.key, newValue);

      // 同步更新 skip_health_check 设置
      if (onSkipHealthCheckChange) {
        onSkipHealthCheckChange(newSkipHealthCheck);
      }
    };

    return (
      <>
        <div className="flex justify-between items-center py-2 border-b border-slate-100 last:border-0 group">
          <div className="flex-1 min-w-0 pr-3">
            <div className="flex items-center gap-2">
              {hasChange && (
                <ChangeDot />
              )}
              <span className="text-xs font-medium text-slate-700">{displayLabel}</span>
              {setting.requires_restart && (
                <span className="inline-flex items-center px-1.5 py-0.5 rounded text-[10px] font-medium bg-amber-100 text-amber-700">
                  <AlertTriangle size={10} className="mr-0.5" />
                  重启生效
                </span>
              )}
            </div>
            {setting.description && (
              <p className="text-[11px] leading-4 text-slate-500 mt-0.5 [overflow-wrap:anywhere]">{setting.description}</p>
            )}
          </div>
          <div className="flex items-center gap-2">
            {/* 显示跳过健康检测状态标签 */}
            {isLoopRetry && skipHealthCheck && (
              <span className="inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-[10px] font-medium bg-indigo-100 text-indigo-700">
                <Timer size={10} />
                跳过检测
              </span>
            )}
            {hasChange && onRevert && (
              <RevertButton
                onClick={() => onRevert(setting.category, setting.key)}
                disabled={disabled}
              />
            )}
            <button
              onClick={() => !disabled && setShowLoopRetrySelector(true)}
              disabled={disabled}
              className={`
                flex items-center gap-2 px-3 py-1.5 rounded-lg border transition-colors
                ${disabled
                  ? 'bg-slate-50 border-slate-200 text-slate-400 cursor-not-allowed'
                  : 'bg-white border-slate-200 hover:border-indigo-300 hover:bg-indigo-50 text-slate-700'
                }
              `}
            >
              <CurrentIcon size={14} className={isLoopRetry ? 'text-indigo-500' : 'text-slate-400'} />
              <span className="text-xs font-medium">{currentLabel}</span>
              <ChevronRight size={14} className="text-slate-400" />
            </button>
          </div>
        </div>

        <LoopRetrySelector
          isOpen={showLoopRetrySelector}
          currentValue={localValue}
          skipHealthCheck={skipHealthCheck}
          onSelect={handleLoopRetrySelect}
          onClose={() => setShowLoopRetrySelector(false)}
        />
      </>
    );
  }

  // 布尔类型使用 Toggle 开关
  if (valueType === 'bool') {
    const isChecked = localValue === 'true' || localValue === true;
    return (
      <div className="flex justify-between items-center py-2 border-b border-slate-100 last:border-0 group">
        <div className="flex-1 min-w-0 pr-3">
          <div className="flex items-center gap-2">
            {hasChange && (
              <ChangeDot />
            )}
            <span className="text-xs font-medium text-slate-700">{displayLabel}</span>
            {setting.requires_restart && (
              <span className="inline-flex items-center px-1.5 py-0.5 rounded text-[10px] font-medium bg-amber-100 text-amber-700">
                <AlertTriangle size={10} className="mr-0.5" />
                重启生效
              </span>
            )}
          </div>
          {setting.description && (
            <p className="text-[11px] leading-4 text-slate-500 mt-0.5 [overflow-wrap:anywhere]">{setting.description}</p>
          )}
        </div>
        <div className="flex items-center gap-2">
          {hasChange && onRevert && (
            <RevertButton
              onClick={() => onRevert(setting.category, setting.key)}
              disabled={disabled}
            />
          )}
          <label className="relative inline-flex min-h-8 items-center cursor-pointer">
            <input
              type="checkbox"
              aria-label={displayLabel}
              checked={isChecked}
              onChange={handleChange}
              disabled={disabled}
              className="sr-only peer"
            />
            <div className={`
              relative w-9 h-5 bg-slate-200 rounded-full peer
              peer-checked:bg-indigo-600
              peer-focus:ring-2 peer-focus:ring-indigo-300
              after:content-[''] after:absolute after:top-0.5 after:left-[2px]
              after:bg-white after:rounded-full after:h-4 after:w-4
              after:transition-all after:shadow-sm
              peer-checked:after:translate-x-full
              ${disabled ? 'opacity-50 cursor-not-allowed' : ''}
            `}></div>
          </label>
        </div>
      </div>
    );
  }

  // 策略类型使用下拉选择
  if (setting.key === 'type' && setting.category === 'strategy') {
    const handleStrategyChange = (newValue) => {
      setLocalValue(newValue);
      onChange(setting.category, setting.key, newValue);
    };

    return (
      <div className="flex justify-between items-center py-2 border-b border-slate-100 last:border-0 group">
        <div className="flex-1 min-w-0 pr-3">
          <div className="flex items-center gap-2">
            {hasChange && (
              <ChangeDot />
            )}
            <span className="text-xs font-medium text-slate-700">{displayLabel}</span>
            {setting.requires_restart && (
              <span className="inline-flex items-center px-1.5 py-0.5 rounded text-[10px] font-medium bg-amber-100 text-amber-700">
                <AlertTriangle size={10} className="mr-0.5" />
                重启生效
              </span>
            )}
          </div>
          {setting.description && (
            <p className="text-[11px] leading-4 text-slate-500 mt-0.5 [overflow-wrap:anywhere]">{setting.description}</p>
          )}
        </div>
        <div className="flex items-center gap-2">
          {hasChange && onRevert && (
            <RevertButton
              onClick={() => onRevert(setting.category, setting.key)}
              disabled={disabled}
            />
          )}
          <CustomSelect
            options={STRATEGY_OPTIONS}
            value={localValue}
            onChange={handleStrategyChange}
            disabled={disabled}
            size="sm"
            ariaLabel={displayLabel}
            className="[&>button]:h-8"
          />
        </div>
      </div>
    );
  }

  // 枚举类型使用下拉选择 (通用枚举类型支持)
  // 优先使用前端强制映射（解决后端 value_type 未及时同步的问题）
  const forcedEnumKey = `${setting.category}.${setting.key}`
  const forcedOptions = FORCED_ENUM_MAP[forcedEnumKey]
  const shouldRenderAsEnum = forcedOptions || (valueType === 'enum' && setting.enum_options && setting.enum_options.length > 0);

  if (shouldRenderAsEnum) {
    const handleEnumChange = (newValue) => {
      setLocalValue(newValue);
      onChange(setting.category, setting.key, newValue);
    };

    // 优先使用前端强制映射，其次使用后端提供的枚举选项
    const enumOptions = forcedOptions || (setting.enum_options || []).map(option => ({
      value: option.value,
      label: option.label
    }));

    // 获取当前选中选项的描述
    const currentOption = (forcedOptions || setting.enum_options)?.find(opt => opt.value === localValue);

    // 通用枚举类型使用下拉选择
    return (
      <div className="py-2 border-b border-slate-100 last:border-0 group">
        <div className="flex justify-between items-center">
          <div className="flex-1 min-w-0 pr-3">
            <div className="flex items-center gap-2">
              {hasChange && (
                <ChangeDot />
              )}
              <span className="text-xs font-medium text-slate-700">{displayLabel}</span>
              {setting.requires_restart && (
                <span className="inline-flex items-center px-1.5 py-0.5 rounded text-[10px] font-medium bg-amber-100 text-amber-700">
                  <AlertTriangle size={10} className="mr-0.5" />
                  重启生效
                </span>
              )}
            </div>
            {setting.description && (
              <p className="text-[11px] leading-4 text-slate-500 mt-0.5 [overflow-wrap:anywhere]">{setting.description}</p>
            )}
          </div>
          <div className="flex items-center gap-2">
            {hasChange && onRevert && (
              <RevertButton
                onClick={() => onRevert(setting.category, setting.key)}
                disabled={disabled}
              />
            )}
            <CustomSelect
              options={enumOptions}
              value={localValue}
              onChange={handleEnumChange}
              disabled={disabled}
              size="sm"
              ariaLabel={displayLabel}
              className="[&>button]:h-8"
            />
          </div>
        </div>
        {/* 当前选项的详细说明 */}
        {currentOption && currentOption.description && (
          <div className="mt-2">
            <div className="p-2 bg-indigo-50/50 border border-indigo-100 rounded-lg">
              <p className="text-xs text-indigo-900 leading-relaxed">{currentOption.description}</p>
            </div>
          </div>
        )}
      </div>
    );
  }

  // duration 类型使用数字+单位选择器
  if (valueType === 'duration') {
    const handleDurationChange = (newValue) => {
      setLocalValue(newValue);
      onChange(setting.category, setting.key, newValue);
    };

    return (
      <div className="flex justify-between items-center py-2 border-b border-slate-100 last:border-0 group">
        <div className="flex-1 min-w-0 pr-3">
          <div className="flex items-center gap-2">
            {hasChange && (
              <ChangeDot />
            )}
            <span className="text-xs font-medium text-slate-700">{displayLabel}</span>
            {setting.requires_restart && (
              <span className="inline-flex items-center px-1.5 py-0.5 rounded text-[10px] font-medium bg-amber-100 text-amber-700">
                <AlertTriangle size={10} className="mr-0.5" />
                重启生效
              </span>
            )}
          </div>
          {setting.description && (
            <p className="text-[11px] leading-4 text-slate-500 mt-0.5 [overflow-wrap:anywhere]">{setting.description}</p>
          )}
        </div>
        <div className="flex items-center gap-2">
          {hasChange && onRevert && (
            <RevertButton
              onClick={() => onRevert(setting.category, setting.key)}
              disabled={disabled}
            />
          )}
          <DurationInput
            value={localValue}
            onChange={handleDurationChange}
            disabled={disabled}
          />
        </div>
      </div>
    );
  }

  // 密码类型（脱敏展示 + 可切换显示）
  if (valueType === 'password') {
    return (
      <div className="flex justify-between items-center py-2 border-b border-slate-100 last:border-0 group">
        <div className="flex-1 min-w-0 pr-3">
          <div className="flex items-center gap-2">
            {hasChange && (
              <ChangeDot />
            )}
            <span className="text-xs font-medium text-slate-700">{displayLabel}</span>
            {setting.requires_restart && (
              <span className="inline-flex items-center px-1.5 py-0.5 rounded text-[10px] font-medium bg-amber-100 text-amber-700">
                <AlertTriangle size={10} className="mr-0.5" />
                重启生效
              </span>
            )}
          </div>
          {setting.description && (
            <p className="text-[11px] leading-4 text-slate-500 mt-0.5 [overflow-wrap:anywhere]">{setting.description}</p>
          )}
        </div>

        <div className="flex items-center gap-2">
          {hasChange && onRevert && (
            <RevertButton
              onClick={() => onRevert(setting.category, setting.key)}
              disabled={disabled}
            />
          )}
          <div className={`relative ${setting.key === 'token' ? 'w-[min(260px,45vw)]' : 'w-[104px]'}`}>
            <input
              type={showSecret ? 'text' : 'password'}
              aria-label={displayLabel}
              value={localValue}
              onChange={handleChange}
              disabled={disabled}
              placeholder={config.placeholder}
              className={`
                h-8 w-full px-2.5 pr-9 bg-slate-50 border border-slate-200 rounded-lg text-xs text-slate-700
                font-mono text-left
                focus:outline-none focus:ring-2 focus:ring-indigo-500 focus:border-indigo-500
                ${disabled ? 'opacity-50 cursor-not-allowed' : ''}
              `}
            />
            <Tooltip content={showSecret ? '隐藏' : '显示'} className="absolute right-2 top-1/2 -translate-y-1/2">
              <button
                type="button"
                onClick={() => setShowSecret(v => !v)}
                disabled={disabled}
                className={`
                  p-1 rounded-md transition-colors
                  ${disabled ? 'text-slate-300 cursor-not-allowed' : 'text-slate-400 hover:text-indigo-600 hover:bg-white/70'}
                `}
                aria-label={showSecret ? '隐藏' : '显示'}
              >
                {showSecret ? <EyeOff size={16} /> : <Eye size={16} />}
              </button>
            </Tooltip>
          </div>
        </div>
      </div>
    );
  }

  // 其他类型使用文本/数字输入框
  const min = setting.category === 'retention' && setting.key === 'retention_days' ? 0 : undefined;

  return (
    <div className="flex justify-between items-center py-2 border-b border-slate-100 last:border-0 group">
      <div className="flex-1 min-w-0 pr-3">
        <div className="flex items-center gap-2">
          {hasChange && (
            <ChangeDot />
          )}
          <span className="text-xs font-medium text-slate-700">{displayLabel}</span>
          {setting.requires_restart && (
            <span className="inline-flex items-center px-1.5 py-0.5 rounded text-[10px] font-medium bg-amber-100 text-amber-700">
              <AlertTriangle size={10} className="mr-0.5" />
              重启生效
            </span>
          )}
        </div>
        {setting.description && (
          <p className="text-[11px] leading-4 text-slate-500 mt-0.5 [overflow-wrap:anywhere]">{setting.description}</p>
        )}
      </div>
      <div className="flex items-center gap-2">
        {hasChange && onRevert && (
          <RevertButton
            onClick={() => onRevert(setting.category, setting.key)}
            disabled={disabled}
          />
        )}
        <div className={`relative ${setting.key === 'token' ? 'w-[min(260px,45vw)]' : 'w-[104px]'}`}>
          <input
            type={config.type}
            aria-label={displayLabel}
            value={localValue}
            onChange={handleChange}
            disabled={disabled}
            step={config.step}
            min={min}
            placeholder={config.placeholder}
            className={`
              h-8 w-full px-2.5 bg-slate-50 border border-slate-200 rounded-lg text-xs text-slate-700
              font-mono text-right
              focus:outline-none focus:ring-2 focus:ring-indigo-500 focus:border-indigo-500
              ${disabled ? 'opacity-50 cursor-not-allowed' : ''}
            `}
          />
        </div>
      </div>
    </div>
  );
};

export default SettingItem;
