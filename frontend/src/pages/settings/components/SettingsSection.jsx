// ============================================
// SettingsSection - 设置分类区块组件
// (2025-12-08)
// ============================================

import { RotateCcw } from 'lucide-react';
import { Tooltip } from '@components/ui';

const SettingsSection = ({
  title,
  icon: Icon,
  children,
  onReset,
  resetDisabled = false,
  className = ''
}) => (
  <div className={`bg-white rounded-2xl border border-slate-200/60 shadow-sm overflow-hidden ${className}`.trim()}>
    <div className="px-6 py-4 bg-slate-50/50 border-b border-slate-100 flex items-center justify-between">
      <div className="flex items-center">
        {Icon && (
          <div className="p-1.5 bg-indigo-100 rounded-lg mr-3">
            <Icon size={16} className="text-indigo-600" />
          </div>
        )}
        <div>
          <h3 className="font-semibold text-slate-900">{title}</h3>
        </div>
      </div>
      {onReset && (
        <Tooltip content="重置为默认值">
          <button
            onClick={onReset}
            disabled={resetDisabled}
            className={`
              inline-flex items-center px-2.5 py-1.5 text-xs font-medium
              text-slate-500 hover:text-slate-700 hover:bg-slate-100
              rounded-lg transition-colors
              ${resetDisabled ? 'opacity-50 cursor-not-allowed' : ''}
            `}
            aria-label="重置为默认值"
          >
            <RotateCcw size={14} className="mr-1" />
            重置
          </button>
        </Tooltip>
      )}
    </div>
    <div className="p-6">
      {children}
    </div>
  </div>
);

export default SettingsSection;
