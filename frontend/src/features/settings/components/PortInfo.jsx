// ============================================
// PortInfo - 端口信息显示组件
// (2025-12-08)
// ============================================

import { CheckCircle2, AlertTriangle, Server } from 'lucide-react';

const PortInfo = ({ portInfo, loading = false }) => {
  if (loading || !portInfo) {
    return (
      <div className="flex items-center gap-3 rounded-lg bg-slate-50 px-3 py-2.5 animate-pulse">
        <div className="h-7 w-7 rounded bg-slate-200"></div>
        <div className="h-4 w-32 rounded bg-slate-200"></div>
      </div>
    );
  }

  const { preferred_port, actual_port, was_occupied } = portInfo;
  const normalizedPort = Number(actual_port);
  const hasValidPort = Number.isInteger(normalizedPort) && normalizedPort > 0;

  return (
    <div className="rounded-lg border border-slate-200/60 bg-slate-50/70 px-3 py-2.5">
      <div className="flex flex-wrap items-center justify-between gap-x-4 gap-y-2">
        <div className="flex min-w-0 items-center gap-2.5">
          <div className="shrink-0 rounded-lg bg-indigo-100 p-1.5">
            <Server size={16} className="text-indigo-600" />
          </div>
          <div>
            <div className="text-xs text-slate-500 font-medium">当前服务端口</div>
            <div className="flex flex-wrap items-center gap-2">
              <span className="text-lg font-semibold leading-6 tabular-nums text-slate-900">{hasValidPort ? normalizedPort : '--'}</span>
              {!hasValidPort ? (
                <span className="inline-flex items-center px-2 py-0.5 rounded-full text-[10px] font-medium bg-rose-100 text-rose-700">
                  <AlertTriangle size={10} className="mr-1" />
                  异常
                </span>
              ) : was_occupied ? (
                <span className="inline-flex items-center px-2 py-0.5 rounded-full text-[10px] font-medium bg-amber-100 text-amber-700">
                  <AlertTriangle size={10} className="mr-1" />
                  端口 {preferred_port} 被占用
                </span>
              ) : (
                <span className="inline-flex items-center px-2 py-0.5 rounded-full text-[10px] font-medium bg-emerald-100 text-emerald-700">
                  <CheckCircle2 size={10} className="mr-1" />
                  正常
                </span>
              )}
            </div>
          </div>
        </div>

        <div className="flex shrink-0 items-center gap-2">
          <span className="text-xs text-slate-500">首选端口</span>
          <span className="text-xs font-mono text-slate-600">{preferred_port}</span>
        </div>
      </div>

      {!hasValidPort && (
        <div className="mt-3 pt-3 border-t border-slate-200/60">
          <p className="text-xs text-rose-600">
            代理服务端口无效（当前值：{String(actual_port)}），服务可能未成功启动。请检查端口占用后重启应用。
          </p>
        </div>
      )}

      {hasValidPort && was_occupied && (
        <div className="mt-3 pt-3 border-t border-slate-200/60">
          <p className="text-xs text-slate-500">
            首选端口 {preferred_port} 已被其他程序占用，系统自动切换到端口 {normalizedPort}。
            如需使用端口 {preferred_port}，请关闭占用该端口的程序后重启应用。
          </p>
        </div>
      )}
    </div>
  );
};

export default PortInfo;
