// ============================================
// 渠道编辑表单的「非流改道」扩展选项（Claude / Codex 共用）
// 语义是来源渠道的能力声明：本渠道不接非流请求，非流请求请送到这些目标。
// 与已删除的「仅流式」开关不同：那个开关只能把非流请求拦在本地，这里是把请求送到能成功的目标
// ============================================

import { useEffect, useState } from 'react';
import { Check } from 'lucide-react';
import { getRouteTargetOptions } from '@/services/wails/index.js';
import RouteTargetsEditor from './RouteTargetsEditor.jsx';
import { getErrorMessage } from '../formatters.js';
import { ROUTE_MODE_OPTIONS } from './routeTargets.js';

const NonStreamRouteEditor = ({ provider, channelId, value, onChange, disabled = false }) => {
  const [options, setOptions] = useState({ channels: [], models: [] });
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState('');

  useEffect(() => {
    let cancelled = false;
    getRouteTargetOptions(provider)
      .then((result) => {
        if (!cancelled) setOptions(result);
      })
      .catch((error) => {
        if (!cancelled) setLoadError(getErrorMessage(error, '加载改道目标失败'));
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [provider]);

  const update = (patch) => onChange({ ...value, ...patch });

  return (
    <div className="space-y-4">
      <div className="rounded-xl border border-sky-100 bg-sky-50/70 px-3.5 py-3 text-xs leading-relaxed text-sky-900">
        本渠道作为当前激活渠道时，把<strong>非流请求</strong>改道到下方目标；流式请求不受影响。
        这是「改道」而不是「拦截」：请求会被送到能处理它的目标完成，而不是在本地被拒绝。
        改道只作用于本次请求，不会切换全局激活渠道。
      </div>

      <div className="flex items-center justify-between gap-4 rounded-lg border border-slate-200 px-3 py-2.5">
        <div className="min-w-0">
          <div className="text-sm font-medium text-slate-700">开启非流改道</div>
          <p className="mt-0.5 text-xs text-slate-400">命中本渠道「模型路由」规则的请求按规则执行，不走这里</p>
        </div>
        <button
          type="button"
          role="switch"
          aria-checked={value.enabled}
          aria-label="开启非流改道"
          onClick={() => update({ enabled: !value.enabled })}
          disabled={disabled}
          className={`relative inline-flex h-6 w-11 flex-shrink-0 rounded-full transition-colors disabled:cursor-not-allowed disabled:opacity-50 ${value.enabled ? 'bg-indigo-600' : 'bg-slate-200'}`}
        >
          <span className={`absolute left-0.5 top-0.5 h-5 w-5 rounded-full bg-white shadow-sm transition-transform ${value.enabled ? 'translate-x-5' : 'translate-x-0'}`} />
        </button>
      </div>

      {value.enabled && (
        <>
          <div className="grid gap-2 sm:grid-cols-2">
            {ROUTE_MODE_OPTIONS.non_stream.map((mode) => {
              const active = value.mode === mode.value;
              return (
                <button
                  key={mode.value}
                  type="button"
                  onClick={() => update({ mode: mode.value })}
                  disabled={disabled}
                  aria-pressed={active}
                  className={`rounded-xl border p-3 text-left transition-colors ${
                    active ? 'border-indigo-300 bg-indigo-50 ring-1 ring-indigo-200' : 'border-slate-200 bg-white hover:border-slate-300'
                  }`}
                >
                  <div className="flex items-center gap-2">
                    <span className={`text-sm font-semibold ${active ? 'text-indigo-800' : 'text-slate-700'}`}>{mode.label}</span>
                    {active && <Check className="ml-auto h-4 w-4 text-indigo-600" />}
                  </div>
                  <p className="mt-1 text-xs leading-relaxed text-slate-500">{mode.description}</p>
                </button>
              );
            })}
          </div>

          <div>
            <div className="mb-2 text-sm font-medium text-slate-700">改道目标（按顺序尝试）</div>
            {loadError ? (
              <p className="rounded-lg border border-rose-200 bg-rose-50 px-3 py-2 text-xs text-rose-700">{loadError}</p>
            ) : (
              <RouteTargetsEditor
                targets={value.targets}
                onChange={(targets) => update({ targets })}
                channels={options.channels}
                models={options.models}
                excludeChannelId={channelId}
                disabled={disabled || loading}
                idPrefix={`${provider}-non-stream-${channelId}`}
              />
            )}
            <p className="mt-2 text-xs leading-relaxed text-slate-400">
              暂停的目标渠道会被跳过。上游模型留空时沿用目标分组的模型策略；跨厂商目标建议显式填写上游模型，并在「基础定价」中补齐它的定价，否则成本会落到兜底价。
            </p>
          </div>
        </>
      )}
    </div>
  );
};

export default NonStreamRouteEditor;
