import { CheckCircle2, Clock, Repeat, ShieldOff, Slash, XCircle } from 'lucide-react';

const RepeatOffIcon = ({ size = 10, className = '' }) => (
  <span className={`relative inline-flex flex-shrink-0 ${className}`} style={{ width: size, height: size }}>
    <Repeat size={size} />
    <Slash size={size} strokeWidth={2.5} className="absolute inset-0" />
  </span>
);

const HEALTH_BADGES = {
  disabled: ['未开启', ShieldOff, 'border-slate-200 bg-slate-50 text-slate-400'],
  healthy: ['健康', CheckCircle2, 'border-emerald-100 bg-emerald-50 text-emerald-600'],
  unhealthy: ['异常', XCircle, 'border-rose-100 bg-rose-50 text-rose-600'],
  skipped: ['跳过检测', ShieldOff, 'border-purple-200 bg-purple-50 text-purple-600'],
  neverChecked: ['未检测', Clock, 'border-slate-200 bg-slate-50 text-slate-400'],
  unchecked: ['检测中', Clock, 'border-slate-200 bg-slate-50 text-slate-500'],
  inactive: ['未接管', Clock, 'border-slate-200 bg-slate-50 text-slate-400'],
  excluded: ['不参与', RepeatOffIcon, 'border-slate-200 bg-slate-50 text-slate-400']
};

export const EndpointHealthBadge = ({ state, healthy, neverChecked, skipped, detail = false }) => {
  const resolvedState = state || (skipped ? 'skipped' : neverChecked ? 'neverChecked' : healthy ? 'healthy' : 'unhealthy');
  const [label, Icon, colorClass] = HEALTH_BADGES[resolvedState] || HEALTH_BADGES.unchecked;

  return (
    <span className={`inline-flex ${detail ? 'h-6 min-w-[64px]' : 'h-5'} w-fit flex-shrink-0 items-center justify-center gap-1 rounded-full border px-1.5 text-[10px] font-medium leading-none whitespace-nowrap ${colorClass}`}>
      <Icon size={10} className="shrink-0" />
      {label}
    </span>
  );
};

export const EndpointLatencyBadge = ({ ms, inScope = true, detail = false, ariaLabel = '最近一次健康探测延迟' }) => {
  if (!inScope) return null;

  const msValue = Number(ms);
  if (!Number.isFinite(msValue) || msValue <= 0) return null;

  const rounded = Math.max(1, Math.round(msValue));
  let colorClass = 'border-emerald-100 bg-emerald-50 text-emerald-600';
  if (rounded > 500) colorClass = 'border-amber-100 bg-amber-50 text-amber-600';
  if (rounded > 1000) colorClass = 'border-rose-100 bg-rose-50 text-rose-600';

  return (
    <span
      className={`inline-flex ${detail ? 'h-6' : 'h-5'} min-w-[54px] flex-shrink-0 items-center justify-center rounded-full border px-1.5 font-mono text-[10px] font-medium leading-none whitespace-nowrap ${colorClass}`}
      aria-label={ariaLabel}
    >
      {rounded}ms
    </span>
  );
};

export const EndpointRequestFailureBadge = ({ failCode, failReason, detail = false, hideWhenEmpty = false }) => {
  const code = Number(failCode || 0);
  const hasCode = Number.isFinite(code) && code >= 100 && code <= 599;
  const hasReason = !!failReason;
  if (hideWhenEmpty && !hasCode && !hasReason) return null;

  const label = [
    hasCode ? `状态码: ${code}` : null,
    hasReason ? `原因: ${failReason}` : null
  ].filter(Boolean).join('，') || '请求失败';

  return (
    <span
      className={`inline-flex ${detail ? 'h-6 min-w-[72px]' : 'h-5'} w-fit flex-shrink-0 items-center justify-center gap-1 rounded-full border border-orange-200 bg-orange-50 px-1.5 text-[10px] font-medium leading-none text-orange-600 whitespace-nowrap`}
      aria-label={label}
    >
      <XCircle size={10} className="shrink-0" />
      请求失败
      {hasCode && <span className="font-mono">({code})</span>}
    </span>
  );
};
