import { formatDuration } from '@utils/api.js';

const FAILED_REQUEST_STATUSES = new Set(['failed', 'error', 'timeout', 'partial_failed']);
const CANCELLED_REQUEST_STATUSES = new Set(['cancelled']);

const getDurationTone = (ms, status) => {
  const seconds = (Number(ms) || 0) / 1000;

  if (FAILED_REQUEST_STATUSES.has(status)) {
    return 'bg-rose-50 text-rose-700 border-rose-200';
  }
  if (CANCELLED_REQUEST_STATUSES.has(status)) {
    return 'bg-slate-50 text-slate-400 border-slate-200';
  }
  if (seconds >= 80) {
    return 'bg-[#FF6900]/12 text-[#FF6900] border-[#FF6900]/30';
  }
  if (seconds >= 40) {
    return 'bg-[#FECA76]/12 text-[#FE9A00] border-[#FECA76]/30';
  }
  return 'bg-emerald-50 text-emerald-700 border-emerald-200';
};

const sizeClasses = {
  sm: 'px-2 py-0.5 rounded-md text-sm',
  xs: 'px-1.5 py-0.5 rounded-full text-[11px]'
};

const DurationBadge = ({ ms, status = '', size = 'sm', className = '' }) => {
  const text = formatDuration(ms);
  const tone = getDurationTone(ms, status);
  const resolvedSize = sizeClasses[size] || sizeClasses.sm;

  return (
    <span className={`inline-flex items-center font-mono border whitespace-nowrap ${resolvedSize} ${tone} ${className}`.trim()}>
      {text}
    </span>
  );
};

export default DurationBadge;
