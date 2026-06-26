import { ArrowRight, Clock, RefreshCw } from 'lucide-react';
import { Tooltip } from '@components/ui';
import ModelTag from '../../requests/components/ModelTag.jsx';

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
    <div className={`${config.root} ${wide ? 'md:col-span-2' : ''}`}>
      <div className={config.header}>
        {Icon && (
          <span className={config.iconBox}>
            <Icon size={config.iconSize} />
          </span>
        )}
        <span className="text-xs font-medium text-slate-500">{label}</span>
      </div>
      <div className="min-w-0">{children}</div>
    </div>
  );
};

export const DetailChip = ({ children, tone = 'slate', mono = false, variant = 'default' }) => (
  <span
    className={`inline-flex min-h-7 items-center justify-center rounded-lg border px-2.5 text-xs font-semibold leading-none ${
      mono ? 'font-mono' : ''
    } ${getToneClass(tone, variant)}`}
  >
    {children}
  </span>
);

export const DetailPill = ({ children, icon: Icon, tone = 'slate', mono = false, variant = 'default' }) => (
  <span
    className={`inline-flex h-6 max-w-full items-center justify-center gap-1 rounded-full border px-1.5 text-[10px] font-medium leading-none whitespace-nowrap align-middle ${
      mono ? 'font-mono' : ''
    } ${getToneClass(tone, variant)}`}
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
  <div className="mt-4 grid grid-cols-1 gap-3 md:grid-cols-2">
    <DetailMetricCard label="创建时间" icon={Clock} density={density}>
      <span className="block truncate font-mono text-sm font-medium text-slate-700">
        {createdAt}
      </span>
    </DetailMetricCard>
    <DetailMetricCard label="更新时间" icon={RefreshCw} density={density}>
      <span className="block truncate font-mono text-sm font-medium text-slate-700">
        {updatedAt}
      </span>
    </DetailMetricCard>
  </div>
);
