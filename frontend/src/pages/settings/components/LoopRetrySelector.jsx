// ============================================
// LoopRetrySelector - 循环重试策略选择弹窗
// v6.2.0 (2026-01-10) - 双选卡片布局
// v6.2.11 (2026-01-11) - 新增跳过定时健康检测开关
// ============================================

import { useState, useEffect } from 'react';
import { X, RefreshCw, XCircle, Check, Timer } from 'lucide-react';

const STRATEGY_OPTIONS = [
  {
    value: 'true',
    label: '循环重试',
    icon: RefreshCw,
    description: '所有渠道耗尽后，回到最高优先级渠道继续尝试，直到达到最大循环轮数',
    benefits: ['持续尝试直到成功', '适合高可用场景', '配合冷却时间避免无效重试'],
    color: 'indigo'
  },
  {
    value: 'false',
    label: '快速失败',
    icon: XCircle,
    description: '所有渠道耗尽后立即返回错误，不再尝试',
    benefits: ['快速响应', '明确错误信号', '适合调试场景'],
    color: 'slate'
  }
];

const LoopRetrySelector = ({ isOpen, currentValue, skipHealthCheck: initialSkipHealthCheck = false, onSelect, onClose }) => {
  const [selectedValue, setSelectedValue] = useState(currentValue);
  const [skipHealthCheck, setSkipHealthCheck] = useState(initialSkipHealthCheck);

  useEffect(() => {
    setSelectedValue(currentValue);
    setSkipHealthCheck(initialSkipHealthCheck);
  }, [currentValue, initialSkipHealthCheck, isOpen]);

  if (!isOpen) return null;

  const handleConfirm = () => {
    // v6.2.11: 返回策略值和跳过健康检测设置
    // 如果选择快速失败，跳过健康检测设置无效，重置为 false
    onSelect(selectedValue, selectedValue === 'true' ? skipHealthCheck : false);
    onClose();
  };

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center">
      {/* Backdrop */}
      <div
        className="absolute inset-0 bg-black/50 backdrop-blur-sm"
        onClick={onClose}
      />

      {/* Dialog */}
      <div className="relative bg-white rounded-2xl shadow-2xl w-full max-w-xl mx-4 overflow-hidden animate-in zoom-in-95 duration-200">
        {/* Header */}
        <div className="px-6 py-4 border-b border-slate-100 flex items-center justify-between">
          <div>
            <h3 className="text-lg font-semibold text-slate-900">选择恢复策略</h3>
            <p className="text-sm text-slate-500 mt-0.5">当所有渠道都耗尽时的处理方式</p>
          </div>
          <button
            onClick={onClose}
            className="p-2 text-slate-400 hover:text-slate-600 hover:bg-slate-100 rounded-lg transition-colors"
          >
            <X size={20} />
          </button>
        </div>

        {/* Options - Horizontal Layout */}
        <div className="p-6">
          <div className="grid grid-cols-2 gap-4">
            {STRATEGY_OPTIONS.map((option) => {
              const Icon = option.icon;
              const isSelected = selectedValue === option.value;
              const colorMap = {
                indigo: {
                  bg: 'bg-indigo-50',
                  border: 'border-indigo-500',
                  ring: 'ring-indigo-500/20',
                  iconBg: 'bg-indigo-100',
                  iconText: 'text-indigo-600',
                  check: 'bg-indigo-500'
                },
                slate: {
                  bg: 'bg-slate-50',
                  border: 'border-slate-400',
                  ring: 'ring-slate-400/20',
                  iconBg: 'bg-slate-100',
                  iconText: 'text-slate-600',
                  check: 'bg-slate-500'
                }
              };
              const colors = colorMap[option.color];

              return (
                <button
                  key={option.value}
                  onClick={() => setSelectedValue(option.value)}
                  className={`
                    relative p-4 rounded-xl border-2 transition-all duration-200 text-left
                    ${isSelected
                      ? `${colors.bg} ${colors.border} ring-4 ${colors.ring}`
                      : 'bg-white border-slate-200 hover:border-slate-300 hover:bg-slate-50'
                    }
                  `}
                >
                  {/* Selection indicator */}
                  {isSelected && (
                    <div className={`absolute top-3 right-3 w-5 h-5 ${colors.check} rounded-full flex items-center justify-center`}>
                      <Check size={12} className="text-white" />
                    </div>
                  )}

                  {/* Icon */}
                  <div className={`
                    w-10 h-10 rounded-lg flex items-center justify-center mb-3
                    ${isSelected ? colors.iconBg : 'bg-slate-100'}
                  `}>
                    <Icon size={20} className={isSelected ? colors.iconText : 'text-slate-500'} />
                  </div>

                  {/* Label */}
                  <h4 className={`font-semibold mb-1 ${isSelected ? 'text-slate-900' : 'text-slate-700'}`}>
                    {option.label}
                  </h4>

                  {/* Description */}
                  <p className="text-xs text-slate-500 mb-3 leading-relaxed">
                    {option.description}
                  </p>

                  {/* Benefits */}
                  <ul className="space-y-1">
                    {option.benefits.map((benefit, idx) => (
                      <li key={idx} className="flex items-center gap-1.5 text-xs text-slate-500">
                        <span className={`w-1 h-1 rounded-full ${isSelected ? colors.check : 'bg-slate-300'}`} />
                        {benefit}
                      </li>
                    ))}
                  </ul>

                  {/* v6.2.11: 循环重试选项内的跳过健康检测开关 */}
                  {option.value === 'true' && isSelected && (
                    <div
                      className="mt-4 pt-3 border-t border-indigo-200"
                      onClick={(e) => e.stopPropagation()}
                    >
                      <label className="flex items-center gap-2 cursor-pointer group">
                        <div className="relative">
                          <input
                            type="checkbox"
                            checked={skipHealthCheck}
                            onChange={(e) => setSkipHealthCheck(e.target.checked)}
                            className="sr-only"
                          />
                          <div className={`
                            w-9 h-5 rounded-full transition-colors duration-200
                            ${skipHealthCheck ? 'bg-indigo-500' : 'bg-slate-300'}
                          `}>
                            <div className={`
                              absolute top-0.5 left-0.5 w-4 h-4 bg-white rounded-full shadow-sm
                              transition-transform duration-200
                              ${skipHealthCheck ? 'translate-x-4' : 'translate-x-0'}
                            `} />
                          </div>
                        </div>
                        <div className="flex-1">
                          <div className="flex items-center gap-1.5">
                            <Timer size={12} className="text-indigo-500" />
                            <span className="text-xs font-medium text-slate-700">跳过定时健康检测</span>
                          </div>
                          <p className="text-[10px] text-slate-400 leading-tight mt-0.5">
                            减少日志噪音，端点状态仅通过实际请求更新
                          </p>
                        </div>
                      </label>
                    </div>
                  )}
                </button>
              );
            })}
          </div>
        </div>

        {/* Footer */}
        <div className="px-6 py-4 bg-slate-50 border-t border-slate-100 flex justify-end gap-3">
          <button
            onClick={onClose}
            className="px-4 py-2 text-sm font-medium text-slate-600 hover:text-slate-800 hover:bg-slate-100 rounded-lg transition-colors"
          >
            取消
          </button>
          <button
            onClick={handleConfirm}
            className="px-4 py-2 text-sm font-medium text-white bg-indigo-600 hover:bg-indigo-700 rounded-lg transition-colors"
          >
            确认选择
          </button>
        </div>
      </div>
    </div>
  );
};

export default LoopRetrySelector;
