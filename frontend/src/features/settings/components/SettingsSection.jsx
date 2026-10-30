// ============================================
// SettingsSection - 设置分类区块组件
// (2025-12-08)
// ============================================

import { RotateCcw } from 'lucide-react';

const SettingsSection = ({
  title,
  icon: Icon,
  description,
  children,
  onReset,
  resetDisabled = false,
  compact = false,
  className = ''
}) => (
  <div className={`settings-category flex min-w-0 flex-col bg-white rounded-xl border border-slate-200/60 shadow-sm overflow-hidden ${className}`.trim()}>
    <div className={`shrink-0 bg-slate-50/50 border-b border-slate-100 flex items-center justify-between gap-2 ${compact ? 'px-3 py-2' : 'px-6 py-4'}`}>
      <div className="flex min-w-0 items-center">
        {Icon && (
          <div className="shrink-0 p-1.5 bg-indigo-50 rounded-lg mr-2.5">
            <Icon size={14} className="text-indigo-600" />
          </div>
        )}
        <div className="min-w-0 [overflow-wrap:anywhere]">
          <h3 className="text-[13px] font-semibold leading-5 text-slate-900">{title}</h3>
          {description && (
            <p className="text-[11px] leading-4 text-slate-500 mt-0.5">{description}</p>
          )}
        </div>
      </div>
      {onReset && (
        <button
          onClick={onReset}
          disabled={resetDisabled}
          className={`
            inline-flex shrink-0 items-center px-2 py-1 text-[11px] font-medium
            text-slate-500 hover:text-slate-700 hover:bg-slate-100
            rounded-lg transition-colors
            ${resetDisabled ? 'opacity-50 cursor-not-allowed' : ''}
          `}
          aria-label="重置为默认值"
        >
          <RotateCcw size={12} className="mr-1" />
          重置
        </button>
      )}
    </div>
    <div className={`flex flex-1 flex-col justify-center ${compact ? 'px-3 py-1' : 'p-6'}`}>
      {children}
    </div>
  </div>
);

export default SettingsSection;
