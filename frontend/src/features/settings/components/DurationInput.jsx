// ============================================
// DurationInput - 时间输入组件（数字+单位选择）
// (2026-01-10) - 简化单位标签、调整下拉框宽度
// ============================================

import { useMemo, useCallback } from 'react';
import { CustomSelect } from '../../../shared/ui/index.js';

// 时间单位选项（简洁标签）
const DURATION_UNITS = [
  { value: 'ms', label: '毫秒' },
  { value: 's', label: '秒' },
  { value: 'm', label: '分钟' },
  { value: 'h', label: '小时' }
];

// 解析 duration 字符串，如 "30s" -> { number: 30, unit: 's' }
const parseDuration = (durationStr) => {
  if (!durationStr || typeof durationStr !== 'string') {
    return { number: '', unit: 's' };
  }

  const match = durationStr.match(/^(\d+(?:\.\d+)?)(ms|s|m|h)$/i);
  if (match) {
    return {
      number: match[1],
      unit: match[2].toLowerCase()
    };
  }

  // 如果只有数字，默认单位为秒
  const numMatch = durationStr.match(/^(\d+(?:\.\d+)?)$/);
  if (numMatch) {
    return { number: numMatch[1], unit: 's' };
  }

  return { number: '', unit: 's' };
};

// 组合为 duration 字符串
const formatDuration = (number, unit) => {
  if (number === '' || number === null || number === undefined) {
    return '';
  }
  return `${number}${unit}`;
};

const DurationInput = ({
  value,
  onChange,
  disabled = false,
  className = ''
}) => {
  const parsed = useMemo(() => parseDuration(value), [value]);

  // 处理数字变更
  const handleNumberChange = useCallback((e) => {
    const newNumber = e.target.value;
    const formatted = formatDuration(newNumber, parsed.unit);
    onChange(formatted);
  }, [onChange, parsed.unit]);

  // 处理单位变更
  const handleUnitChange = useCallback((newUnit) => {
    const formatted = formatDuration(parsed.number, newUnit);
    onChange(formatted);
  }, [onChange, parsed.number]);

  return (
    <div className={`flex h-8 w-[136px] shrink-0 items-center overflow-visible rounded-lg border border-slate-200 bg-slate-50 transition-[border-color,box-shadow] focus-within:border-indigo-500 focus-within:ring-2 focus-within:ring-indigo-500/20 ${disabled ? 'opacity-50' : ''} ${className}`}>
      <input
        type="number"
        min="0"
        step="1"
        value={parsed.number}
        onChange={handleNumberChange}
        disabled={disabled}
        placeholder="0"
        aria-label="时长数值"
        className={`min-w-0 flex-1 bg-transparent px-2.5 text-right font-mono text-xs text-slate-700 outline-none ${disabled ? 'cursor-not-allowed' : ''}`}
      />
      <span className="h-5 w-px flex-shrink-0 bg-slate-200" aria-hidden="true" />
      <CustomSelect
        options={DURATION_UNITS}
        value={parsed.unit}
        onChange={handleUnitChange}
        disabled={disabled}
        size="sm"
        minWidth={64}
        embedded
        ariaLabel="时间单位"
      />
    </div>
  );
};

export default DurationInput;
