import { useId, useState } from 'react';
import { Activity, ArrowRight, Clock, Loader2, RefreshCw, ShieldCheck, ShieldOff, X } from 'lucide-react';
import { Tooltip } from '@/shared/ui/index.js';
import { renderModalPortal } from '@/shared/ui/modalPortal.js';
import ModelTag from '../../../requests/components/ModelTag.jsx';
import './resourceDetails.css';

const DETAIL_CHIP_TONES = {
  default: {
    slate: 'border-slate-200 bg-slate-50 text-slate-500',
    emerald: 'border-emerald-200 bg-emerald-50 text-emerald-700',
    amber: 'border-amber-200 bg-amber-50 text-amber-700',
    rose: 'border-rose-200 bg-rose-50 text-rose-700',
    purple: 'border-purple-200 bg-purple-50 text-purple-700',
    indigo: 'border-indigo-200 bg-indigo-50 text-indigo-700',
    sky: 'border-sky-200 bg-sky-50 text-sky-700'
  },
  codex: {
    slate: 'border-slate-200 bg-white text-slate-700',
    emerald: 'border-emerald-200 bg-emerald-50 text-emerald-700',
    amber: 'border-amber-200 bg-amber-50 text-amber-700',
    rose: 'border-rose-200 bg-rose-50 text-rose-700',
    purple: 'border-purple-200 bg-purple-50 text-purple-700',
    indigo: 'border-indigo-200 bg-indigo-50 text-indigo-700',
    sky: 'border-sky-200 bg-sky-50 text-sky-700'
  }
};

const METRIC_CARD_DENSITY = {
  normal: {
    root: 'min-w-0 rounded-xl border border-slate-200/70 bg-slate-50 p-3.5',
    header: 'mb-2 flex items-center gap-2',
    iconBox: 'inline-flex h-6 w-6 items-center justify-center rounded-lg bg-slate-100 text-slate-500',
    iconSize: 13
  },
  compact: {
    root: 'min-w-0 h-full rounded-xl border border-slate-200/70 bg-slate-50 p-3',
    header: 'mb-1.5 flex items-center gap-2',
    iconBox: 'inline-flex h-5 w-5 items-center justify-center rounded-md bg-slate-100 text-slate-500',
    iconSize: 12
  }
};

const getToneClass = (tone, variant) => {
  const toneMap = DETAIL_CHIP_TONES[variant] || DETAIL_CHIP_TONES.default;
  return toneMap[tone] || toneMap.slate;
};

export const DetailMetricCard = ({ label, icon: Icon, wide = false, density = 'normal', children }) => {
  const config = METRIC_CARD_DENSITY[density] || METRIC_CARD_DENSITY.normal;

  return (
    <section className={`rd-card ${config.root} ${wide ? 'md:col-span-2' : ''}`}>
      <div className={`rd-card-heading ${config.header}`}>
        {Icon && (
          <span className={config.iconBox}>
            <Icon size={config.iconSize} />
          </span>
        )}
        <h3 className="text-xs font-medium text-slate-500">{label}</h3>
      </div>
      <div className="min-w-0">{children}</div>
    </section>
  );
};

export const DetailChip = ({ children, tone = 'slate', mono = false, variant = 'default' }) => (
  <span
    className={`rd-chip inline-flex min-h-7 max-w-full flex-wrap items-center justify-center rounded-lg border px-2.5 text-xs font-semibold leading-none ${
      mono ? 'font-mono' : ''
    } ${getToneClass(tone, variant)}`}
  >
    {children}
  </span>
);

export const DetailPill = ({ children, icon: Icon, tone = 'slate', mono = false, variant = 'default', className = '' }) => (
  <span
    className={`inline-flex h-6 max-w-full items-center justify-center gap-1 rounded-full border px-1.5 text-[10px] font-medium leading-none whitespace-nowrap align-middle ${
      mono ? 'font-mono' : ''
    } ${getToneClass(tone, variant)} ${className}`}
  >
    {Icon && <Icon size={10} className="shrink-0" />}
    <span className="inline-flex min-w-0 items-center justify-center gap-1 leading-none">
      {children}
    </span>
  </span>
);

export const DetailTimeoutPill = ({ custom, value, tooltip, variant = 'default' }) => (
  <Tooltip content={tooltip}>
    <span className="inline-flex">
      <DetailPill tone={custom ? 'amber' : 'sky'} icon={Clock} variant={variant}>
        <span className="leading-none">{custom ? '超时' : '默认'}</span>
        <span className="font-mono leading-none">{value}</span>
      </DetailPill>
    </span>
  </Tooltip>
);

const MODEL_MAPPING_TAG_CLASS = 'max-w-full [&>span]:whitespace-normal [&>span]:break-all';

export const ModelMappingPair = ({ sourceModel, targetModel }) => (
  <div className="flex min-h-7 min-w-0 flex-wrap items-center justify-start gap-2 py-0.5">
    <ModelTag model={sourceModel} className={MODEL_MAPPING_TAG_CLASS} />
    <ArrowRight size={14} className="shrink-0 text-slate-400" />
    <ModelTag model={targetModel} className={MODEL_MAPPING_TAG_CLASS} />
  </div>
);

export const DetailTimestampFooter = ({ createdAt, updatedAt, density = 'normal' }) => (
  <footer className="rd-timestamps" data-density={density}>
    <span><Clock size={11} />创建时间 <time>{createdAt}</time></span>
    <span><RefreshCw size={11} />更新时间 <time>{updatedAt}</time></span>
  </footer>
);

export const ResourceDetailShell = ({ title, subtitle, eyebrow, icon: Icon, tone = 'endpoint', actions, summary, onClose, children }) => {
  const titleId = useId();
  return renderModalPortal(
    <div className="rd-backdrop fixed inset-0 z-[110] flex items-center justify-center bg-slate-900/40 p-4 backdrop-blur-sm" onMouseDown={(event) => {
      if (event.target === event.currentTarget) onClose?.();
    }}>
      <section role="dialog" aria-modal="true" aria-labelledby={titleId} data-detail-tone={tone} className="resource-detail-dialog flex max-h-[calc(100dvh-2rem)] w-full max-w-3xl flex-col overflow-hidden rounded-2xl bg-white shadow-xl">
        <header className="rd-header">
          <div className="rd-heading">
            <span className="rd-emblem">{Icon && <Icon size={19} />}</span>
            <div className="min-w-0 flex-1">
              <p className="rd-eyebrow">{eyebrow}</p>
              <h2 id={titleId} className="rd-title">{title}</h2>
            </div>
            <div className="rd-actions">
              {actions}
              <Tooltip asChild content="关闭"><button type="button" className="rd-close" aria-label="关闭" onClick={onClose}><X size={16} /></button></Tooltip>
            </div>
          </div>
          {subtitle && <div className="rd-subtitle">{subtitle}</div>}
          {summary && <div className="rd-summary">{summary}</div>}
        </header>
        <div className="rd-body min-h-0 flex-1 overflow-y-auto">{children}</div>
      </section>
    </div>
  );
};

export const EndpointHealthSection = ({ skipped, subscription, inScope = true, ms, lastCheck, onCheck, onToggle, disabled = false }) => {
  const [pending, setPending] = useState('');
  const [error, setError] = useState('');
  const run = async (kind, callback) => {
    if (pending || disabled) return;
    setPending(kind);
    setError('');
    try { await callback(); }
    catch (err) { setError(err?.message || String(err)); }
    finally { setPending(''); }
  };
  const latency = Number(ms);
  const hasLatency = inScope && !subscription && Number.isFinite(latency) && latency > 0;
  return <section className="rd-health" aria-label="健康检查">
    <div className="rd-health-heading">
      <h3><Activity size={13} />健康检查</h3>
      {onCheck && !subscription && <button type="button" className="rd-check-button" disabled={disabled || !!pending} onClick={() => run('check', onCheck)}>
        {pending === 'check' ? <Loader2 size={12} className="animate-spin" /> : <RefreshCw size={12} />}{pending === 'check' ? '检测中…' : '立即检测'}
      </button>}
    </div>
    <dl className="rd-health-metrics">
      <div><dt>探测延迟</dt><dd className="rd-latency" data-tone={!hasLatency ? 'empty' : latency > 1000 ? 'slow' : latency > 500 ? 'medium' : 'fast'}>{hasLatency ? <>{Math.max(1, Math.round(latency))}<small>ms</small></> : <span>{!inScope ? '未开启' : subscription ? '不主动测速' : '未测速'}</span>}</dd></div>
      <div><dt>检测方式</dt><dd>{!inScope ? '自动检测未开启' : subscription ? '真实请求验证' : skipped ? '仅测速' : '健康探测'}</dd></div>
      <div><dt>最近检测</dt><dd className="rd-health-time">{!inScope ? '—' : lastCheck && lastCheck !== '-' ? lastCheck : '暂无检测记录'}</dd></div>
    </dl>
    {!subscription && onToggle && <div className="rd-health-toggle">
      <span className="rd-health-toggle-icon">{skipped ? <ShieldOff size={15} /> : <ShieldCheck size={15} />}</span>
      <span className="min-w-0 flex-1"><strong>跳过健康检查</strong><span>{inScope ? '跳过健康判定，仍保留自动测速。' : '加入当前默认方案的启用成员后生效。'}</span></span>
      <button type="button" role="switch" aria-label="跳过健康检查" aria-checked={inScope && !!skipped} className="rd-health-switch" disabled={!inScope || disabled || !!pending} onClick={() => run('toggle', () => onToggle(!skipped))}><span /></button>
    </div>}
    <p className="rd-health-note">{!inScope ? '未参与当前默认方案，不发送后台探测。立即检测仅执行一次，不开启自动检测。' : subscription ? '订阅端点通过真实请求更新状态，不发送主动探测。' : '仅对当前默认方案的启用成员自动探测。延迟不代表模型首字节或生成耗时。'}</p>
    {error && <p className="rd-inline-error" role="alert">{error}</p>}
  </section>;
};
