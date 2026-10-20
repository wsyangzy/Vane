// ============================================
// EndpointAvailabilityBar - 端点可用性时间条
// ============================================
// status page 风格：一排等宽时间片，颜色表示该小时内真实请求的成功率。
//
// 数据取自请求记录而非健康探测——探测通但转发失败的端点并不少见。
// 代价是没有流量的小时没有数据，这些格子渲染为灰色空档而不是省略：
// 省略会让时间轴不再等距，读起来像是那段时间一切正常。

import { useCallback, useEffect, useState } from 'react';
import { Tooltip } from '@/shared/ui/index.js';
import { getEndpointAvailability } from '@/services/wails/index.js';

const SLICE_STATES = {
  empty: ['无数据', 'bg-slate-100'],
  healthy: ['正常', 'bg-emerald-500'],
  degraded: ['降级', 'bg-amber-400'],
  down: ['故障', 'bg-rose-500']
};

// 小时级分片下 99% 约合 36 秒不可用，仍算正常；低于 80% 视为故障而非抖动
const resolveSliceState = (slice) => {
  if (!slice?.hasData) return 'empty';
  if (slice.availability >= 99) return 'healthy';
  if (slice.availability >= 80) return 'degraded';
  return 'down';
};

const formatPercent = (value) => `${Number(value || 0).toFixed(1)}%`;
const formatMs = (value) => `${Math.max(0, Math.round(Number(value || 0)))}ms`;

const EndpointAvailabilityBar = ({ provider = '', channel = '', endpointName = '', hours = 24 }) => {
  const [data, setData] = useState(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');

  const loadData = useCallback(async () => {
    if (!endpointName) {
      setLoading(false);
      return;
    }
    setLoading(true);
    setError('');
    try {
      setData(await getEndpointAvailability({ provider, channel, endpointName, hours }));
    } catch (err) {
      setError(err?.message || '加载可用性数据失败');
    } finally {
      setLoading(false);
    }
  }, [provider, channel, endpointName, hours]);

  useEffect(() => {
    loadData();
  }, [loadData]);

  const slices = data?.slices || [];
  // 汇总同时作为无障碍替代：时间片本身只有几像素宽，达不到最小点击目标，
  // 键盘与读屏用户从这里获得等价信息（WCAG 2.5.8 的 Equivalent 例外）
  const summaryLabel = data?.hasData
    ? `最近 ${hours} 小时可用率 ${formatPercent(data.availability)}，共 ${data.total} 个请求，${data.failed} 个失败`
    : `最近 ${hours} 小时没有请求记录`;

  return (
    <div className="flex flex-col gap-2 rounded-lg border border-slate-200 bg-white px-3 py-2.5">
      <div className="flex items-end justify-between gap-3">
        <div className="flex min-w-0 flex-col">
          <span className="text-[10px] font-semibold uppercase tracking-[0.16em] text-slate-400">
            Availability
          </span>
          <span className="text-[11px] text-slate-500">最近 {hours} 小时 · 按小时</span>
        </div>
        <div className="flex flex-shrink-0 items-baseline gap-1">
          <span
            className={`font-mono text-xl font-semibold leading-none ${
              data?.hasData ? 'text-emerald-600' : 'text-slate-300'
            }`}
          >
            {data?.hasData ? formatPercent(data.availability) : '—'}
          </span>
        </div>
      </div>

      {error ? (
        <div className="py-2 text-center text-[11px] text-rose-500">{error}</div>
      ) : (
        <div role="img" aria-label={summaryLabel} className="flex h-8 items-stretch gap-px">
          {(loading ? Array.from({ length: hours }, () => null) : slices).map((slice, index) => {
            if (!slice) {
              return (
                <div
                  key={`skeleton-${index}`}
                  className="min-w-0 flex-1 animate-pulse rounded-[1px] bg-slate-100 first:rounded-l-[4px] last:rounded-r-[4px]"
                />
              );
            }
            const state = resolveSliceState(slice);
            const [stateLabel, colorClass] = SLICE_STATES[state];
            return (
              <Tooltip
                key={slice.key || index}
                className="min-w-0 flex-1"
                content={
                  <span className="flex flex-col gap-0.5">
                    <span className="font-semibold">{slice.label}</span>
                    <span>状态: {stateLabel}</span>
                    {slice.hasData && (
                      <>
                        <span>可用率: {formatPercent(slice.availability)}</span>
                        <span>首字节: {formatMs(slice.avgFirstByteMs)}</span>
                        <span className="text-slate-300">
                          请求 {slice.total} · 失败 {slice.failed}
                        </span>
                      </>
                    )}
                  </span>
                }
              >
                <div
                  className={`h-full w-full rounded-[1px] transition-opacity duration-150 hover:opacity-70 ${colorClass}`}
                />
              </Tooltip>
            );
          })}
        </div>
      )}

      <div className="flex items-center justify-between text-[10px] text-slate-400">
        <span>{hours} 小时前</span>
        {data?.hasData && (
          <span className="font-mono">平均首字节 {formatMs(data.avgFirstByteMs)}</span>
        )}
        <span>现在</span>
      </div>
    </div>
  );
};

export default EndpointAvailabilityBar;
