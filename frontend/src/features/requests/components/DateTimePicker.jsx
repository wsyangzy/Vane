// ============================================
// DateTimeRangePicker - 单次提交的日期时间范围选择器
// ============================================

import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { CalendarRange, ChevronDown, ChevronLeft, ChevronRight, Clock } from 'lucide-react';
import { CustomSelect } from '@/shared/ui/index.js';
import {
  formatDateTimeLocal,
  formatDisplayDate,
  formatDisplayDateTime,
  isDayWithinRange,
  isSameDay,
  isValidDateTimeRange,
  parseDateTimeLocal
} from '../utils/dateTimeRange.js';

const WEEKDAY_LABELS = ['一', '二', '三', '四', '五', '六', '日'];
const VIEWPORT_MARGIN = 16;

const pad2 = (value) => String(value).padStart(2, '0');
const monthLabel = (date) => `${date.getFullYear()}年${date.getMonth() + 1}月`;
const startOfMonth = (date) => new Date(date.getFullYear(), date.getMonth(), 1);
const addMonths = (date, amount) => new Date(date.getFullYear(), date.getMonth() + amount, 1);

const createDefaultDate = (hour, minute) => {
  const now = new Date();
  return new Date(now.getFullYear(), now.getMonth(), now.getDate(), hour, minute, 0);
};

const copyDateWithTime = (pickedDate, sourceDate) => new Date(
  pickedDate.getFullYear(),
  pickedDate.getMonth(),
  pickedDate.getDate(),
  sourceDate.getHours(),
  sourceDate.getMinutes(),
  0
);

const createDraftRange = (startValue, endValue) => {
  const parsedStart = parseDateTimeLocal(startValue);
  const parsedEnd = parseDateTimeLocal(endValue);
  const start = parsedStart || (parsedEnd
    ? new Date(parsedEnd.getFullYear(), parsedEnd.getMonth(), parsedEnd.getDate(), 0, 0, 0)
    : createDefaultDate(0, 0));
  const end = parsedEnd || (parsedStart
    ? new Date(parsedStart.getFullYear(), parsedStart.getMonth(), parsedStart.getDate(), 23, 59, 0)
    : createDefaultDate(23, 59));

  return { start, end };
};

const buildMonthGrid = (viewDate) => {
  const firstOfMonth = startOfMonth(viewDate);
  const mondayBasedIndex = (firstOfMonth.getDay() + 6) % 7;
  const startDate = new Date(
    firstOfMonth.getFullYear(),
    firstOfMonth.getMonth(),
    1 - mondayBasedIndex
  );

  return Array.from({ length: 42 }, (_, index) => {
    const date = new Date(startDate);
    date.setDate(startDate.getDate() + index);
    return {
      date,
      day: date.getDate(),
      isOutside: date.getMonth() !== viewDate.getMonth(),
      isToday: isSameDay(date, new Date())
    };
  });
};

const BoundaryControl = ({
  label,
  value,
  isActive,
  onActivate,
  onTimeChange,
  hourOptions,
  minuteOptions
}) => (
  <div
    className={`min-w-0 p-3 transition-colors ${
      isActive ? 'bg-indigo-50/60' : 'bg-white hover:bg-slate-50/70'
    }`}
  >
    <div className="mb-2 flex items-center justify-between gap-2">
      <span className={`text-[11px] font-semibold ${isActive ? 'text-indigo-600' : 'text-slate-400'}`}>
        {label}
      </span>
      <span className="text-[10px] text-slate-400">{isActive ? '正在选择' : ''}</span>
    </div>
    <div className="flex min-w-0 items-center gap-2">
      <button
        type="button"
        onClick={onActivate}
        className={`h-8 min-w-0 flex-1 rounded-md border px-2 text-left font-mono text-xs tabular-nums transition-colors focus:outline-none focus-visible:ring-2 focus-visible:ring-indigo-200 ${
          isActive
            ? 'border-indigo-300 bg-white text-indigo-700'
            : 'border-slate-200 bg-white text-slate-600 hover:border-slate-300'
        }`}
      >
        {formatDisplayDate(value)}
      </button>
      <Clock className="size-3.5 flex-shrink-0 text-slate-400" />
      <CustomSelect
        options={hourOptions}
        value={String(value.getHours())}
        onChange={(hour) => onTimeChange(Number(hour), value.getMinutes())}
        size="xs"
        minWidth={48}
        ariaLabel={`${label}小时`}
      />
      <span className="text-xs text-slate-400">:</span>
      <CustomSelect
        options={minuteOptions}
        value={String(value.getMinutes())}
        onChange={(minute) => onTimeChange(value.getHours(), Number(minute))}
        size="xs"
        minWidth={48}
        ariaLabel={`${label}分钟`}
      />
    </div>
  </div>
);

const CalendarMonth = ({
  viewDate,
  draftStart,
  draftEnd,
  onSelect,
  onPrevious,
  onNext,
  showPrevious,
  showNext
}) => {
  const cells = useMemo(() => buildMonthGrid(viewDate), [viewDate]);

  return (
    <div className="min-w-0 p-3 sm:p-4">
      <div className="mb-3 grid h-8 grid-cols-[32px_minmax(0,1fr)_32px] items-center">
        {showPrevious ? (
          <button
            type="button"
            className="inline-flex size-8 items-center justify-center rounded-md text-slate-500 transition-colors hover:bg-slate-100 hover:text-slate-700 focus:outline-none focus-visible:ring-2 focus-visible:ring-indigo-200"
            onClick={onPrevious}
            aria-label="上个月"
          >
            <ChevronLeft className="size-4" />
          </button>
        ) : <span />}
        <div className="text-center text-sm font-semibold text-slate-800">{monthLabel(viewDate)}</div>
        {showNext ? (
          <button
            type="button"
            className="inline-flex size-8 items-center justify-center rounded-md text-slate-500 transition-colors hover:bg-slate-100 hover:text-slate-700 focus:outline-none focus-visible:ring-2 focus-visible:ring-indigo-200"
            onClick={onNext}
            aria-label="下个月"
          >
            <ChevronRight className="size-4" />
          </button>
        ) : <span />}
      </div>

      <div className="mb-1 grid grid-cols-7 gap-0.5">
        {WEEKDAY_LABELS.map((label) => (
          <div key={label} className="py-1 text-center text-[11px] font-medium text-slate-400">
            {label}
          </div>
        ))}
      </div>

      <div className="grid grid-cols-7 gap-0.5">
        {cells.map((cell) => {
          const isStart = !cell.isOutside && isSameDay(cell.date, draftStart);
          const isEnd = !cell.isOutside && isSameDay(cell.date, draftEnd);
          const isBoundary = isStart || isEnd;
          const isWithinRange = !cell.isOutside && isDayWithinRange(cell.date, draftStart, draftEnd);
          const dateLabel = `${formatDisplayDate(cell.date)}${isStart ? '，开始时间' : ''}${isEnd ? '，结束时间' : ''}`;
          const colorClass = isBoundary
            ? 'bg-indigo-600 text-white shadow-sm shadow-indigo-200'
            : isWithinRange
              ? 'bg-indigo-50 text-indigo-700'
              : cell.isOutside
                ? 'text-slate-300 hover:bg-slate-50'
                : 'text-slate-600 hover:bg-indigo-50 hover:text-indigo-700';
          const todayClass = cell.isToday && !isBoundary ? 'ring-1 ring-inset ring-indigo-200' : '';

          return (
            <button
              key={formatDisplayDate(cell.date)}
              type="button"
              onClick={() => onSelect(cell.date)}
              className={`h-8 rounded-md text-xs tabular-nums transition-colors focus:outline-none focus-visible:ring-2 focus-visible:ring-indigo-200 ${colorClass} ${todayClass}`}
              aria-label={dateLabel}
              aria-pressed={isBoundary}
            >
              {cell.day}
            </button>
          );
        })}
      </div>
    </div>
  );
};

const DateTimeRangePicker = ({
  startValue,
  endValue,
  onChange,
  disabled = false,
  inputClassName = ''
}) => {
  const containerRef = useRef(null);
  const panelRef = useRef(null);
  const closeTimerRef = useRef(null);
  const initialRange = createDraftRange(startValue, endValue);
  const [draftStart, setDraftStart] = useState(initialRange.start);
  const [draftEnd, setDraftEnd] = useState(initialRange.end);
  const [baseMonth, setBaseMonth] = useState(() => startOfMonth(initialRange.start));
  const [activeBoundary, setActiveBoundary] = useState('start');
  const [isOpen, setIsOpen] = useState(false);
  const [isRendered, setIsRendered] = useState(false);
  const [dropdownState, setDropdownState] = useState('');

  const selectedStart = useMemo(() => parseDateTimeLocal(startValue), [startValue]);
  const selectedEnd = useMemo(() => parseDateTimeLocal(endValue), [endValue]);
  const displayStart = selectedStart ? formatDisplayDateTime(selectedStart) : '开始时间';
  const displayEnd = selectedEnd ? formatDisplayDateTime(selectedEnd) : '结束时间';
  const isValidRange = isValidDateTimeRange(draftStart, draftEnd);
  const visibleMonths = useMemo(() => [baseMonth, addMonths(baseMonth, 1)], [baseMonth]);
  const hourOptions = useMemo(
    () => Array.from({ length: 24 }, (_, hour) => ({ value: String(hour), label: pad2(hour) })),
    []
  );
  const minuteOptions = useMemo(
    () => Array.from({ length: 60 }, (_, minute) => ({ value: String(minute), label: pad2(minute) })),
    []
  );

  const closePanel = useCallback(() => {
    if (closeTimerRef.current) window.clearTimeout(closeTimerRef.current);
    setIsOpen(false);
    setDropdownState('is-closing');
    const closeDuration = parseFloat(
      getComputedStyle(document.documentElement).getPropertyValue('--dropdown-close-dur')
    ) || 150;
    closeTimerRef.current = window.setTimeout(() => {
      setIsRendered(false);
      setDropdownState('');
    }, closeDuration);
  }, []);

  const openPanel = useCallback(() => {
    if (closeTimerRef.current) window.clearTimeout(closeTimerRef.current);
    const nextRange = createDraftRange(startValue, endValue);
    setDraftStart(nextRange.start);
    setDraftEnd(nextRange.end);
    setBaseMonth(startOfMonth(nextRange.start));
    setActiveBoundary('start');
    setDropdownState('');
    setIsRendered(true);
    setIsOpen(true);
  }, [endValue, startValue]);

  useEffect(() => {
    if (!isOpen || !isRendered) return undefined;

    const frameId = window.requestAnimationFrame(() => setDropdownState('is-open'));
    return () => {
      window.cancelAnimationFrame(frameId);
    };
  }, [isOpen, isRendered]);

  useEffect(() => () => {
    if (closeTimerRef.current) window.clearTimeout(closeTimerRef.current);
  }, []);

  useEffect(() => {
    if (!isOpen) return undefined;

    const handleMouseDown = (event) => {
      if (containerRef.current && !containerRef.current.contains(event.target)) closePanel();
    };
    const handleKeyDown = (event) => {
      if (event.key === 'Escape') closePanel();
    };

    document.addEventListener('mousedown', handleMouseDown);
    window.addEventListener('keydown', handleKeyDown);
    return () => {
      document.removeEventListener('mousedown', handleMouseDown);
      window.removeEventListener('keydown', handleKeyDown);
    };
  }, [closePanel, isOpen]);

  useEffect(() => {
    if (!isRendered) return undefined;

    const updatePosition = () => {
      const panel = panelRef.current;
      const anchor = panel?.offsetParent;
      if (!panel || !anchor) return;

      const anchorLeft = anchor.getBoundingClientRect().left;
      const panelRight = anchorLeft + panel.offsetWidth;
      const nextOffset = anchorLeft < VIEWPORT_MARGIN
        ? VIEWPORT_MARGIN - anchorLeft
        : Math.min(window.innerWidth - VIEWPORT_MARGIN - panelRight, 0);
      panel.style.translate = `${nextOffset}px 0`;
    };

    let frameId;
    const refreshPosition = () => {
      updatePosition();
      window.cancelAnimationFrame(frameId);
      frameId = window.requestAnimationFrame(updatePosition);
    };
    const animatedAncestor = containerRef.current?.closest('.animate-in');
    refreshPosition();
    window.addEventListener('resize', refreshPosition);
    window.addEventListener('scroll', refreshPosition, true);
    animatedAncestor?.addEventListener('animationend', refreshPosition);
    return () => {
      window.cancelAnimationFrame(frameId);
      window.removeEventListener('resize', refreshPosition);
      window.removeEventListener('scroll', refreshPosition, true);
      animatedAncestor?.removeEventListener('animationend', refreshPosition);
    };
  }, [isRendered]);

  const handleDateSelect = useCallback((pickedDate) => {
    if (activeBoundary === 'start') {
      const nextStart = copyDateWithTime(pickedDate, draftStart);
      setDraftStart(nextStart);
      if (nextStart > draftEnd) setDraftEnd(copyDateWithTime(pickedDate, draftEnd));
      setActiveBoundary('end');
      return;
    }

    const nextEnd = copyDateWithTime(pickedDate, draftEnd);
    if (nextEnd < draftStart) {
      setDraftEnd(copyDateWithTime(draftStart, draftEnd));
      setDraftStart(copyDateWithTime(pickedDate, draftStart));
    } else {
      setDraftEnd(nextEnd);
    }
    setActiveBoundary('start');
  }, [activeBoundary, draftEnd, draftStart]);

  const updateTime = useCallback((boundary, hour, minute) => {
    const source = boundary === 'start' ? draftStart : draftEnd;
    const next = new Date(
      source.getFullYear(),
      source.getMonth(),
      source.getDate(),
      hour,
      minute,
      0
    );
    if (boundary === 'start') setDraftStart(next);
    else setDraftEnd(next);
    setActiveBoundary(boundary);
  }, [draftEnd, draftStart]);

  const handleConfirm = () => {
    if (!isValidRange) return;
    onChange?.({
      startValue: formatDateTimeLocal(draftStart),
      endValue: formatDateTimeLocal(draftEnd)
    });
    closePanel();
  };

  const handleClear = () => {
    onChange?.({ startValue: '', endValue: '' });
    closePanel();
  };

  return (
    <div className="relative min-w-0" ref={containerRef}>
      <button
        type="button"
        disabled={disabled}
        onClick={() => {
          if (disabled) return;
          if (isOpen) closePanel();
          else openPanel();
        }}
        className={`${inputClassName} group inline-flex min-w-0 items-center gap-2 text-left tabular-nums transition-colors disabled:cursor-not-allowed disabled:opacity-50`}
        aria-haspopup="dialog"
        aria-expanded={isOpen}
      >
        <CalendarRange className="size-4 flex-shrink-0 text-slate-400 transition-colors group-hover:text-indigo-500" />
        <span className="min-w-0 flex-1 truncate text-center font-mono text-xs sm:text-sm">{displayStart}</span>
        <span className="flex-shrink-0 text-xs text-slate-400">至</span>
        <span className="min-w-0 flex-1 truncate text-center font-mono text-xs sm:text-sm">{displayEnd}</span>
        <ChevronDown className={`size-4 flex-shrink-0 text-slate-400 transition-transform ${isOpen ? 'rotate-180' : ''}`} />
      </button>

      {isRendered && (
        <div
          ref={panelRef}
          data-origin="top-left"
          className={`t-dropdown ${dropdownState} absolute left-0 top-full z-[80] mt-2 w-[min(680px,calc(100vw-2rem))] max-h-[calc(100vh-2rem)] overflow-y-auto rounded-lg border border-slate-200 bg-white shadow-xl ring-1 ring-black/5`}
          role="dialog"
          aria-label="选择时间范围"
        >
          <div className="grid grid-cols-1 divide-y divide-slate-100 border-b border-slate-100 sm:grid-cols-2 sm:divide-x sm:divide-y-0">
            <BoundaryControl
              label="开始时间"
              value={draftStart}
              isActive={activeBoundary === 'start'}
              onActivate={() => setActiveBoundary('start')}
              onTimeChange={(hour, minute) => updateTime('start', hour, minute)}
              hourOptions={hourOptions}
              minuteOptions={minuteOptions}
            />
            <BoundaryControl
              label="结束时间"
              value={draftEnd}
              isActive={activeBoundary === 'end'}
              onActivate={() => setActiveBoundary('end')}
              onTimeChange={(hour, minute) => updateTime('end', hour, minute)}
              hourOptions={hourOptions}
              minuteOptions={minuteOptions}
            />
          </div>

          <div className="grid grid-cols-1 divide-y divide-slate-100 sm:grid-cols-2 sm:divide-x sm:divide-y-0">
            <CalendarMonth
              viewDate={visibleMonths[0]}
              draftStart={draftStart}
              draftEnd={draftEnd}
              onSelect={handleDateSelect}
              onPrevious={() => setBaseMonth((month) => addMonths(month, -1))}
              showPrevious
            />
            <CalendarMonth
              viewDate={visibleMonths[1]}
              draftStart={draftStart}
              draftEnd={draftEnd}
              onSelect={handleDateSelect}
              onNext={() => setBaseMonth((month) => addMonths(month, 1))}
              showNext
            />
          </div>

          <div className="flex min-h-14 items-center justify-between gap-3 border-t border-slate-100 px-4 py-2.5">
            <div className="min-w-0 text-xs text-rose-600" role="status" aria-live="polite">
              {isValidRange ? '' : '开始时间不能晚于结束时间'}
            </div>
            <div className="flex flex-shrink-0 items-center gap-2">
              <button
                type="button"
                className="h-8 px-3 text-xs text-slate-500 transition-colors hover:text-slate-800 focus:outline-none focus-visible:ring-2 focus-visible:ring-indigo-200"
                onClick={handleClear}
              >
                清空
              </button>
              <button
                type="button"
                className="h-8 rounded-md border border-slate-200 bg-white px-3 text-xs text-slate-600 transition-colors hover:bg-slate-50 focus:outline-none focus-visible:ring-2 focus-visible:ring-indigo-200"
                onClick={closePanel}
              >
                取消
              </button>
              <button
                type="button"
                disabled={!isValidRange}
                className="h-8 rounded-md bg-slate-900 px-4 text-xs font-medium text-white shadow-sm transition-colors hover:bg-slate-800 focus:outline-none focus-visible:ring-2 focus-visible:ring-slate-300 disabled:cursor-not-allowed disabled:opacity-40"
                onClick={handleConfirm}
              >
                确定
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
};

export default DateTimeRangePicker;
