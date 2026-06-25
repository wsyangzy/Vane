// ============================================
// 优先级编辑器组件
// 用于编辑端点优先级
// 2025-11-28
// ============================================

import { useState, useRef, useImperativeHandle, forwardRef } from 'react';
import { Minus, Plus } from 'lucide-react';

const normalizePriority = (priority, fallback = 0) => {
  const numeric = Number(priority);
  return Number.isFinite(numeric) ? Math.max(0, Math.floor(numeric)) : fallback;
};

/**
 * 优先级编辑器组件
 * @param {number} priority - 当前优先级
 * @param {string} endpointName - 端点名称
 * @param {Function} onUpdate - 更新回调 (endpointName, newPriority) => Promise
 */
const PriorityEditor = forwardRef(({ priority = 0, endpointName, onUpdate }, ref) => {
  const currentPriority = normalizePriority(priority);
  const [value, setValue] = useState(currentPriority);
  const [isUpdating, setIsUpdating] = useState(false);
  const inputRef = useRef(null);

  // 暴露给父组件的方法
  useImperativeHandle(ref, () => ({
    executeUpdate: handleUpdate,
    isUpdating,
    getCurrentValue: () => value
  }));

  // 值变化是否需要更新
  const hasChanged = value !== currentPriority;

  // 增加优先级
  const handleIncrease = () => {
    setValue(prev => prev + 1);
  };

  // 减少优先级
  const handleDecrease = () => {
    if (value > 0) {
      setValue(prev => prev - 1);
    }
  };

  // 手动输入
  const handleInputChange = (e) => {
    setValue(normalizePriority(e.target.value));
  };

  // 执行更新
  const handleUpdate = async () => {
    if (!hasChanged || isUpdating || !onUpdate) return;

    setIsUpdating(true);
    try {
      const result = await onUpdate(endpointName, value);
      if (result?.success) {
        // 更新成功，保持当前值
      } else {
        // 更新失败，回退值
        setValue(currentPriority);
      }
    } catch (error) {
      console.error('优先级更新失败:', error);
      setValue(currentPriority);
    } finally {
      setIsUpdating(false);
    }
  };

  // 键盘事件
  const handleKeyDown = (e) => {
    if (e.key === 'Enter') {
      handleUpdate();
    } else if (e.key === 'Escape') {
      setValue(currentPriority);
    }
  };

  return (
    <div className="inline-flex items-center gap-0.5">
      {/* 减少按钮 */}
      <button
        onClick={handleDecrease}
        disabled={value <= 0 || isUpdating}
        className={`
          p-1 rounded transition-colors
          ${value <= 0 || isUpdating
            ? 'text-slate-300 cursor-not-allowed'
            : 'text-slate-400 hover:text-slate-600 hover:bg-slate-100'}
        `}
        aria-label="降低优先级"
      >
        <Minus size={12} />
      </button>

      {/* 输入框 */}
      <input
        ref={inputRef}
        type="number"
        min="0"
        value={value}
        onChange={handleInputChange}
        onKeyDown={handleKeyDown}
        onBlur={() => {
          if (hasChanged) handleUpdate();
        }}
        disabled={isUpdating}
        className={`
          w-10 h-7 text-center text-xs font-semibold rounded border transition-all
          ${hasChanged
            ? 'border-amber-300 bg-amber-50 text-amber-700'
            : 'border-slate-200 bg-slate-50 text-slate-600'}
          ${isUpdating ? 'opacity-50' : ''}
          focus:outline-none focus:ring-2 focus:ring-indigo-200 focus:border-indigo-400
        `}
        aria-label={hasChanged ? '有未保存的更改' : '当前优先级'}
      />

      {/* 增加按钮 */}
      <button
        onClick={handleIncrease}
        disabled={isUpdating}
        className={`
          p-1 rounded transition-colors
          ${isUpdating
            ? 'text-slate-300 cursor-not-allowed'
            : 'text-slate-400 hover:text-slate-600 hover:bg-slate-100'}
        `}
        aria-label="提高优先级"
      >
        <Plus size={12} />
      </button>

      {/* 更新指示器 */}
      {hasChanged && !isUpdating && (
        <span className="ml-1 w-1.5 h-1.5 rounded-full bg-amber-400 animate-pulse" />
      )}
    </div>
  );
});

PriorityEditor.displayName = 'PriorityEditor';

export default PriorityEditor;
