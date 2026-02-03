// ============================================
// DurationInput - 时间输入组件（数字+单位选择）
// v6.2.2 (2026-01-10) - 简化单位标签、调整下拉框宽度
// ============================================

import { useState, useEffect, useCallback } from 'react';
import { CustomSelect } from '../../../components/ui';

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
  const [parsed, setParsed] = useState(() => parseDuration(value));

  // 同步外部 value 变化
  useEffect(() => {
    const newParsed = parseDuration(value);
    setParsed(newParsed);
  }, [value]);

  // 处理数字变更
  const handleNumberChange = useCallback((e) => {
    const newNumber = e.target.value;
    setParsed(prev => {
      const newParsed = { ...prev, number: newNumber };
      // 触发外部更新
      const formatted = formatDuration(newNumber, prev.unit);
      if (formatted) {
        onChange(formatted);
      }
      return newParsed;
    });
  }, [onChange]);

  // 处理单位变更
  const handleUnitChange = useCallback((newUnit) => {
    setParsed(prev => {
      const newParsed = { ...prev, unit: newUnit };
      // 触发外部更新
      const formatted = formatDuration(prev.number, newUnit);
      if (formatted) {
        onChange(formatted);
      }
      return newParsed;
    });
  }, [onChange]);

  return (
    <div className={`flex items-center gap-1.5 ${className}`}>
      <input
        type="number"
        min="0"
        step="1"
        value={parsed.number}
        onChange={handleNumberChange}
        disabled={disabled}
        placeholder="0"
        className={`
          w-20 px-3 py-1.5 bg-slate-50 border border-slate-200 rounded-lg text-sm text-slate-700
          font-mono text-right
          focus:outline-none focus:ring-2 focus:ring-indigo-500 focus:border-indigo-500
          ${disabled ? 'opacity-50 cursor-not-allowed' : ''}
        `}
      />
      <CustomSelect
        options={DURATION_UNITS}
        value={parsed.unit}
        onChange={handleUnitChange}
        disabled={disabled}
        size="sm"
        minWidth={56}
      />
    </div>
  );
};

export default DurationInput;
