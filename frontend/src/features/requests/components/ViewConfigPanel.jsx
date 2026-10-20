// ============================================
// ViewConfigPanel - 列显示配置面板
// 2025-12-01 09:30:08
// ============================================

import { X, Columns, Eye, EyeOff, Check } from 'lucide-react';

/**
 * ViewConfigPanel - 列显示配置弹出面板
 * @param {Object} props
 * @param {boolean} props.isOpen - 是否打开
 * @param {Function} props.onClose - 关闭回调
 * @param {Array} props.columns - 所有列配置
 * @param {Array} props.visibleColumns - 当前可见的列ID数组
 * @param {Function} props.onToggleColumn - 切换列显示回调
 * @param {Function} props.onReset - 重置回调
 */
const ViewConfigPanel = ({
  isOpen,
  onClose,
  columns = [],
  visibleColumns = [],
  onToggleColumn,
  onReset
}) => {
  if (!isOpen) return null;

  return (
    <div className="absolute top-12 right-0 z-30 w-64 bg-white rounded-xl shadow-xl border border-gray-100 ring-1 ring-black/5 animate-in fade-in zoom-in-95 slide-in-from-top-2 duration-200 origin-top-right">
      {/* 头部 */}
      <div className="px-4 py-3 border-b border-gray-50 flex justify-between items-center bg-gray-50/50 rounded-t-xl">
        <div className="flex items-center gap-2 text-sm font-semibold text-gray-800">
          <Columns className="w-3.5 h-3.5 text-indigo-500" />
          <span>显示列配置</span>
        </div>
        <button
          onClick={onClose}
          className="p-1 hover:bg-gray-200 rounded-md transition-colors"
        >
          <X className="w-3.5 h-3.5 text-gray-400" />
        </button>
      </div>

      {/* 列选项列表 */}
      <div className="p-2 overflow-y-auto max-h-[300px]">
        {columns.map(col => {
          const isVisible = visibleColumns.includes(col.id);
          return (
            <label
              key={col.id}
              className={`group flex items-center justify-between px-3 py-2 rounded-lg cursor-pointer transition-all ${
                col.alwaysVisible
                  ? 'opacity-50 cursor-not-allowed bg-gray-50'
                  : 'hover:bg-gray-50 active:scale-[0.98]'
              }`}
            >
              <div className="flex items-center gap-3">
                <input
                  type="checkbox"
                  checked={isVisible}
                  disabled={col.alwaysVisible}
                  onChange={() => !col.alwaysVisible && onToggleColumn?.(col.id)}
                  className="peer sr-only"
                />
                <span
                  aria-hidden="true"
                  className={`inline-flex h-4 w-4 shrink-0 items-center justify-center rounded-md border transition-all ${
                    isVisible
                      ? col.alwaysVisible
                        ? 'border-slate-200 bg-slate-100 text-slate-300'
                        : 'border-indigo-500 bg-indigo-600 text-white shadow-sm shadow-indigo-500/20'
                      : 'border-slate-300 bg-white text-transparent group-hover:border-indigo-300'
                  } ${
                    col.alwaysVisible
                      ? ''
                      : 'peer-focus-visible:ring-2 peer-focus-visible:ring-indigo-200 peer-focus-visible:ring-offset-1'
                  }`}
                >
                  {isVisible && <Check className="h-3 w-3" strokeWidth={3} />}
                </span>
                <span className={`text-sm ${isVisible ? 'text-gray-700 font-medium' : 'text-gray-400'}`}>
                  {col.label}
                </span>
              </div>
              {isVisible ? (
                <Eye className="w-3.5 h-3.5 text-gray-400" />
              ) : (
                <EyeOff className="w-3.5 h-3.5 text-gray-300" />
              )}
            </label>
          );
        })}
      </div>

      {/* 底部重置按钮 */}
      <div className="p-3 border-t border-gray-50 bg-gray-50/30 rounded-b-xl flex justify-center">
        <button
          onClick={onReset}
          className="text-xs text-indigo-600 font-medium hover:text-indigo-800 hover:underline transition-colors"
        >
          恢复默认设置
        </button>
      </div>

      {/* 装饰箭头 */}
      <div className="absolute -top-1.5 right-11 w-3 h-3 bg-white border-l border-t border-gray-100 transform rotate-45"></div>
    </div>
  );
};

export default ViewConfigPanel;
