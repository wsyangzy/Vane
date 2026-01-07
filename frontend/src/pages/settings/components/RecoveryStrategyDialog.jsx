import React from 'react';
import { X, Zap, RefreshCw, XCircle, CheckCircle, AlertTriangle, Info } from 'lucide-react';

/**
 * 恢复策略选择弹窗
 * 提供策略对比和详细说明，帮助用户做出更明智的选择
 */
export default function RecoveryStrategyDialog({ isOpen, onClose, currentValue, onConfirm }) {
  const [selectedStrategy, setSelectedStrategy] = React.useState(currentValue || 'auto_wait');

  React.useEffect(() => {
    if (isOpen) {
      setSelectedStrategy(currentValue || 'auto_wait');
    }
  }, [isOpen, currentValue]);

  if (!isOpen) return null;

  const strategies = [
    {
      value: 'auto_wait',
      name: '智能等待',
      icon: Zap,
      iconColor: 'text-emerald-600',
      bgColor: 'bg-emerald-50',
      borderColor: 'border-emerald-200',
      badge: '推荐',
      badgeColor: 'bg-emerald-500',
      description: '所有渠道耗尽后，挂起请求并智能等待端点自愈或渠道恢复',
      scenario: '生产环境、稳定性优先',
      pros: [
        '避免循环重试浪费资源',
        '减少无效请求和计费',
        '自动恢复机制智能',
        '系统负载更可控'
      ],
      cons: [
        '可能等待较长时间（最多 auto_wait_timeout）',
        '需要配置超时和最大挂起数'
      ],
      configTips: '建议配置：超时 300s，最大挂起 100 个请求'
    },
    {
      value: 'loop_retry',
      name: '循环重试',
      icon: RefreshCw,
      iconColor: 'text-amber-600',
      bgColor: 'bg-amber-50',
      borderColor: 'border-amber-200',
      badge: '仅测试',
      badgeColor: 'bg-amber-500',
      description: '所有渠道耗尽后，立即回到最高优先级渠道继续尝试（跳过冷却中的端点）',
      scenario: '测试环境、渠道极少场景',
      pros: [
        '持续尝试，不会超时',
        '适合渠道数量少的场景',
        '实现简单'
      ],
      cons: [
        '可能在渠道间循环重试，浪费资源',
        '增加系统负载和计费成本',
        '不适合生产环境'
      ],
      configTips: '⚠️ 生产环境慎用！可能导致循环重试和资源浪费'
    },
    {
      value: 'fail_fast',
      name: '快速失败',
      icon: XCircle,
      iconColor: 'text-rose-600',
      bgColor: 'bg-rose-50',
      borderColor: 'border-rose-200',
      badge: '调试用',
      badgeColor: 'bg-rose-500',
      description: '所有渠道耗尽后立即返回错误，不等待恢复，不循环重试',
      scenario: '调试环境、需要明确错误信号',
      pros: [
        '立即返回明确错误',
        '快速反馈，便于调试',
        '不浪费资源'
      ],
      cons: [
        '可能错过即将恢复的端点',
        '用户体验较差（频繁失败）',
        '不适合生产环境'
      ],
      configTips: '仅用于调试和开发环境，生产环境不推荐'
    }
  ];

  const handleConfirm = () => {
    onConfirm(selectedStrategy);
    onClose();
  };

  return (
    <div
      className="fixed inset-0 bg-black/50 backdrop-blur-sm z-[60] flex items-start justify-center pt-28 pb-6 px-6"
      onClick={onClose}
    >
      <div
        className="bg-white rounded-xl shadow-2xl max-w-2xl w-full max-h-[calc(100vh-8rem)] overflow-hidden flex flex-col"
        onClick={(e) => e.stopPropagation()}
      >
        {/* 头部 */}
        <div className="px-5 py-3.5 border-b border-slate-200 flex items-center justify-between bg-gradient-to-r from-slate-50 to-white">
          <div>
            <h2 className="text-base font-semibold text-slate-900">选择恢复策略</h2>
            <p className="text-xs text-slate-500 mt-0.5">所有渠道耗尽后的请求处理策略</p>
          </div>
          <button
            onClick={onClose}
            className="p-1.5 text-slate-400 hover:text-slate-600 hover:bg-slate-100 rounded-lg transition-colors"
          >
            <X size={18} />
          </button>
        </div>

        {/* 策略卡片 */}
        <div className="flex-1 overflow-y-auto p-4 min-h-0">
          <div className="space-y-3">
            {strategies.map((strategy) => {
              const Icon = strategy.icon;
              const isSelected = selectedStrategy === strategy.value;

              return (
                <div
                  key={strategy.value}
                  onClick={() => setSelectedStrategy(strategy.value)}
                  className={`
                    relative p-4 rounded-lg border-2 cursor-pointer transition-all
                    ${isSelected
                      ? `${strategy.borderColor} ${strategy.bgColor} shadow-md scale-[1.01]`
                      : 'border-slate-200 bg-white hover:border-slate-300 hover:shadow-sm'
                    }
                  `}
                >
                  {/* 选中指示器 */}
                  {isSelected && (
                    <div className="absolute top-3 right-3">
                      <CheckCircle size={20} className="text-emerald-500" />
                    </div>
                  )}

                  {/* 头部 */}
                  <div className="flex items-start gap-2.5 mb-2.5">
                    <div className={`p-2 rounded-lg ${strategy.bgColor}`}>
                      <Icon size={20} className={strategy.iconColor} />
                    </div>
                    <div className="flex-1 pr-7">
                      <div className="flex items-center gap-2">
                        <h3 className="text-sm font-semibold text-slate-900">{strategy.name}</h3>
                        <span className={`px-1.5 py-0.5 rounded-full text-[10px] font-medium text-white ${strategy.badgeColor}`}>
                          {strategy.badge}
                        </span>
                      </div>
                      <p className="text-xs text-slate-600 mt-1 leading-relaxed">{strategy.description}</p>
                    </div>
                  </div>

                  {/* 适用场景 */}
                  <div className="mb-2.5 pl-10">
                    <div className="flex items-center gap-1.5 text-xs text-slate-500">
                      <Info size={12} />
                      <span>适用：{strategy.scenario}</span>
                    </div>
                  </div>

                  {/* 优缺点 - 更紧凑的单列布局 */}
                  <div className="space-y-2 pl-10">
                    {/* 优点 */}
                    <div>
                      <div className="flex items-center gap-1.5 mb-1">
                        <CheckCircle size={12} className="text-emerald-500" />
                        <span className="text-xs font-medium text-slate-700">优点</span>
                      </div>
                      <div className="flex flex-wrap gap-1.5">
                        {strategy.pros.slice(0, 2).map((pro, idx) => (
                          <span key={idx} className="inline-flex items-center px-2 py-0.5 rounded-md bg-emerald-50 text-[11px] text-emerald-700">
                            {pro}
                          </span>
                        ))}
                      </div>
                    </div>

                    {/* 缺点 */}
                    <div>
                      <div className="flex items-center gap-1.5 mb-1">
                        <AlertTriangle size={12} className="text-amber-500" />
                        <span className="text-xs font-medium text-slate-700">注意</span>
                      </div>
                      <div className="flex flex-wrap gap-1.5">
                        {strategy.cons.slice(0, 1).map((con, idx) => (
                          <span key={idx} className="inline-flex items-center px-2 py-0.5 rounded-md bg-amber-50 text-[11px] text-amber-700">
                            {con}
                          </span>
                        ))}
                      </div>
                    </div>
                  </div>

                  {/* 配置提示 */}
                  <div className="mt-2.5 pt-2.5 border-t border-slate-200 pl-10">
                    <p className="text-[11px] text-slate-500 leading-relaxed">
                      <span className="font-medium">💡 </span>
                      {strategy.configTips}
                    </p>
                  </div>
                </div>
              );
            })}
          </div>
        </div>

        {/* 底部操作 */}
        <div className="px-5 py-3 border-t border-slate-200 flex items-center justify-between bg-slate-50">
          <div className="text-xs text-slate-500">
            当前: <span className="font-medium text-slate-700">
              {strategies.find(s => s.value === selectedStrategy)?.name}
            </span>
          </div>
          <div className="flex items-center gap-2.5">
            <button
              onClick={onClose}
              className="px-3.5 py-1.5 text-xs font-medium text-slate-700 bg-white border border-slate-300 rounded-lg hover:bg-slate-50 transition-colors"
            >
              取消
            </button>
            <button
              onClick={handleConfirm}
              className="px-3.5 py-1.5 text-xs font-medium text-white bg-indigo-600 rounded-lg hover:bg-indigo-700 transition-colors shadow-sm"
            >
              确认选择
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}
