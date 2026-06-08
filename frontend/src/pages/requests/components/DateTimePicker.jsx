// ============================================
// DateTimePicker - 自定义日期时间选择器（替代原生 datetime-local，以便美化日历弹层）
// 2025-12-29
// ============================================

import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Calendar, ChevronLeft, ChevronRight, Clock, X } from 'lucide-react';
import { CustomSelect } from '@components/ui';

const WEEKDAY_LABELS = ['一', '二', '三', '四', '五', '六', '日'];

const pad2 = (value) => String(value).padStart(2, '0');

const parseDateTimeLocal = (value) => {
  if (!value || typeof value !== 'string') return null;
  const [datePart, timePart] = value.split('T');
  if (!datePart || !timePart) return null;

  const [year, month, day] = datePart.split('-').map(Number);
  const [hour, minute] = timePart.split(':').slice(0, 2).map(Number);
  if (!year || !month || !day) return null;

  return new Date(year, month - 1, day, hour || 0, minute || 0, 0);
};

const formatDateTimeLocal = (date) => {
  const year = date.getFullYear();
  const month = pad2(date.getMonth() + 1);
  const day = pad2(date.getDate());
  const hour = pad2(date.getHours());
  const minute = pad2(date.getMinutes());
  return `${year}-${month}-${day}T${hour}:${minute}`;
};

const formatDisplayDateTime = (date) => {
  const year = date.getFullYear();
  const month = pad2(date.getMonth() + 1);
  const day = pad2(date.getDate());
  const hour = pad2(date.getHours());
  const minute = pad2(date.getMinutes());
  return `${year}-${month}-${day} ${hour}:${minute}`;
};

const monthLabel = (date) => `${date.getFullYear()}年${date.getMonth() + 1}月`;

const isSameDay = (a, b) =>
  a &&
  b &&
  a.getFullYear() === b.getFullYear() &&
  a.getMonth() === b.getMonth() &&
  a.getDate() === b.getDate();

const buildMonthGrid = (viewDate) => {
  const year = viewDate.getFullYear();
  const month = viewDate.getMonth();
  const firstOfMonth = new Date(year, month, 1);

  const mondayBasedIndex = (firstOfMonth.getDay() + 6) % 7;
  const startDate = new Date(year, month, 1 - mondayBasedIndex);

  const cells = [];
  for (let i = 0; i < 42; i++) {
    const d = new Date(startDate);
    d.setDate(startDate.getDate() + i);
    const inCurrentMonth = d.getMonth() === month && d.getFullYear() === year;
    cells.push({
      date: d,
      inCurrentMonth,
      day: d.getDate(),
      isOutside: !inCurrentMonth,
      isToday: isSameDay(d, new Date())
    });
  }

  return cells;
};

/**
 * DateTimePicker
 * @param {Object} props
 * @param {string} props.value - datetime-local 格式：YYYY-MM-DDTHH:mm（可选带秒）
 * @param {(value: string) => void} props.onChange - 值变更回调
 * @param {boolean} props.disabled - 禁用
 * @param {{hour:number, minute:number}} props.defaultTime - value 为空时的默认时间
 * @param {string} props.inputClassName - 触发输入框样式（复用现有样式，避免影响布局）
 */
const DateTimePicker = ({
  value,
  onChange,
  disabled = false,
  defaultTime = { hour: 0, minute: 0 },
  inputClassName = ''
}) => {
  const containerRef = useRef(null);
  const [isOpen, setIsOpen] = useState(false);

  const selectedDate = useMemo(() => parseDateTimeLocal(value), [value]);
  const [viewDate, setViewDate] = useState(() => selectedDate || new Date());

  useEffect(() => {
    const onKeyDown = (e) => {
      if (!isOpen) return;
      if (e.key === 'Escape') setIsOpen(false);
    };
    window.addEventListener('keydown', onKeyDown);
    return () => window.removeEventListener('keydown', onKeyDown);
  }, [isOpen]);

  useEffect(() => {
    const handleClickOutside = (event) => {
      if (containerRef.current && !containerRef.current.contains(event.target)) {
        setIsOpen(false);
      }
    };
    document.addEventListener('mousedown', handleClickOutside);
      return () => document.removeEventListener('mousedown', handleClickOutside);
  }, []);

  const cells = useMemo(() => buildMonthGrid(viewDate), [viewDate]);

  const safeBaseDate = useMemo(() => {
    if (selectedDate) return selectedDate;
    const now = new Date();
    return new Date(
      now.getFullYear(),
      now.getMonth(),
      now.getDate(),
      defaultTime.hour || 0,
      defaultTime.minute || 0,
      0
    );
  }, [defaultTime.hour, defaultTime.minute, selectedDate]);

  const setDatePart = useCallback(
    (pickedDate) => {
      const next = new Date(
        pickedDate.getFullYear(),
        pickedDate.getMonth(),
        pickedDate.getDate(),
        safeBaseDate.getHours(),
        safeBaseDate.getMinutes(),
        0
      );
      onChange?.(formatDateTimeLocal(next));
    },
    [onChange, safeBaseDate]
  );

  const setTimePart = useCallback(
    (hour, minute) => {
      const base = safeBaseDate;
      const next = new Date(
        base.getFullYear(),
        base.getMonth(),
        base.getDate(),
        hour,
        minute,
        0
      );
      onChange?.(formatDateTimeLocal(next));
    },
    [onChange, safeBaseDate]
  );

  const hourValue = selectedDate ? selectedDate.getHours() : defaultTime.hour || 0;
  const minuteValue = selectedDate ? selectedDate.getMinutes() : defaultTime.minute || 0;
  const displayValue = selectedDate ? formatDisplayDateTime(selectedDate) : '';
  const hourOptions = useMemo(
    () => Array.from({ length: 24 }).map((_, i) => ({ value: String(i), label: pad2(i) })),
    []
  );
  const minuteOptions = useMemo(
    () => Array.from({ length: 60 }).map((_, i) => ({ value: String(i), label: pad2(i) })),
    []
  );

  return (
    <div className="relative" ref={containerRef}>
      <Calendar className="pointer-events-none absolute left-2.5 top-1/2 -translate-y-1/2 w-4 h-4 text-gray-400" />
      <input
        type="text"
        readOnly
        disabled={disabled}
        value={displayValue}
        onClick={() => {
          if (disabled) return;
          if (!isOpen) setViewDate(selectedDate || new Date());
          setIsOpen(!isOpen);
        }}
        className={`${inputClassName} pr-9 text-center tabular-nums`}
        aria-haspopup="dialog"
        aria-expanded={isOpen}
      />

      {isOpen && (
        <div className="absolute top-full left-0 mt-2 z-50 w-[320px] bg-white border border-gray-200 rounded-xl shadow-xl ring-1 ring-black/5 p-3">
          <div className="flex items-center justify-between mb-3">
            <div className="flex items-center gap-2">
              <button
                type="button"
                className="p-1.5 rounded-lg hover:bg-gray-50 text-gray-600 transition-colors focus:outline-none focus-visible:ring-2 focus-visible:ring-indigo-100"
                onClick={() => setViewDate((d) => new Date(d.getFullYear(), d.getMonth() - 1, 1))}
              >
                <ChevronLeft className="w-4 h-4" />
              </button>
              <div className="text-sm font-semibold text-gray-900 min-w-[110px] text-center">
                {monthLabel(viewDate)}
              </div>
              <button
                type="button"
                className="p-1.5 rounded-lg hover:bg-gray-50 text-gray-600 transition-colors focus:outline-none focus-visible:ring-2 focus-visible:ring-indigo-100"
                onClick={() => setViewDate((d) => new Date(d.getFullYear(), d.getMonth() + 1, 1))}
              >
                <ChevronRight className="w-4 h-4" />
              </button>
            </div>
            <button
              type="button"
              className="p-1.5 rounded-lg hover:bg-gray-50 text-gray-500 transition-colors focus:outline-none focus-visible:ring-2 focus-visible:ring-indigo-100"
              onClick={() => setIsOpen(false)}
              aria-label="关闭"
            >
              <X className="w-4 h-4" />
            </button>
          </div>

          <div className="grid grid-cols-7 gap-1 mb-2">
            {WEEKDAY_LABELS.map((label) => (
              <div key={label} className="text-[11px] text-gray-400 font-medium text-center py-1">
                {label}
              </div>
            ))}
          </div>

          <div className="grid grid-cols-7 gap-1">
            {cells.map((cell) => {
              const isSelected = selectedDate ? isSameDay(cell.date, selectedDate) : false;
              const baseClasses = 'h-9 w-9 rounded-lg text-sm transition-colors focus:outline-none focus-visible:ring-2 focus-visible:ring-indigo-100';
              const todayRing = cell.isToday && !isSelected ? 'ring-1 ring-indigo-200' : '';
              const selectedClasses = isSelected
                ? 'bg-indigo-600 text-white shadow-sm'
                : cell.isOutside
                  ? 'text-gray-300 hover:bg-gray-50'
                  : 'text-gray-700 hover:bg-indigo-50 hover:text-indigo-700';

              return (
                <button
                  key={`${cell.date.toISOString()}-${cell.day}`}
                  type="button"
                  className={`${baseClasses} ${selectedClasses} ${todayRing}`}
                  onClick={() => setDatePart(cell.date)}
                >
                  {cell.day}
                </button>
              );
            })}
          </div>

          <div className="mt-3 pt-3 border-t border-gray-100 flex items-center justify-between gap-2">
            <div className="flex items-center gap-2">
              <Clock className="w-4 h-4 text-gray-400" />
              <CustomSelect
                options={hourOptions}
                value={String(hourValue)}
                onChange={(nextValue) => setTimePart(Number(nextValue), minuteValue)}
                size="xs"
                minWidth={58}
              />
              <span className="text-gray-400 text-sm">:</span>
              <CustomSelect
                options={minuteOptions}
                value={String(minuteValue)}
                onChange={(nextValue) => setTimePart(hourValue, Number(nextValue))}
                size="xs"
                minWidth={58}
              />
            </div>

            <div className="flex items-center gap-2">
              <button
                type="button"
                className="h-9 px-3 text-sm text-gray-600 bg-white border border-gray-200 rounded-lg hover:bg-gray-50 transition-colors"
                onClick={() => onChange?.('')}
              >
                清空
              </button>
              <button
                type="button"
                className="h-9 px-3 text-sm text-white bg-slate-900 rounded-lg hover:bg-slate-800 shadow-sm shadow-slate-200 transition-colors"
                onClick={() => setIsOpen(false)}
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

export default DateTimePicker;
