// ============================================
// TokenUsageHeatmapPanel - 请求追踪 Token 活动分布
// ============================================

import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Activity, RefreshCw, X } from 'lucide-react';
import { Tooltip, useToast } from '@/shared/ui/index.js';
import { fetchTokenUsageHeatmapData } from '@/services/http/api.js';

const HEAT_LEVELS = [
  'border-slate-100 bg-slate-100',
  'border-sky-100 bg-sky-100',
  'border-sky-200 bg-sky-200',
  'border-sky-300 bg-sky-300',
  'border-blue-400 bg-blue-400',
  'border-blue-600 bg-blue-600'
];

const HEATMAP_CELL_SIZE = 12;
const HEATMAP_CELL_GAP = 3;
const HEATMAP_ROWS = 7;
const HEATMAP_MIN_COLUMNS = 50;
const HEATMAP_AXIS_DATE_LABELS = 4;
const HEATMAP_AXIS_HOUR_LABELS = 4;

const formatTokens = (value) => {
  const number = Number(value || 0);
  if (number >= 1000000) return `${(number / 1000000).toFixed(1)}M`;
  if (number >= 1000) return `${(number / 1000).toFixed(1)}K`;
  return number.toLocaleString();
};

const heatLevel = (item) => {
  if (!item || Number(item.total || 0) <= 0) return 0;
  return Math.max(1, Math.min(5, Math.ceil(Number(item.intensity || 0) * 5)));
};

const getBucketKind = (items) => items.find(item => item?.bucket)?.bucket || 'day';

const dateLabel = (key, showYear = false) => {
  if (!key) return '';
  const value = String(key);
  const label = `${Number(value.slice(5, 7))}.${Number(value.slice(8, 10))}`;
  return showYear ? `${value.slice(0, 4)}.${label}` : label;
};

const fullDateLabel = (key) => {
  if (!key) return '';
  return dateLabel(key, true);
};

const hourLabel = (key) => `${Number(String(key).slice(11, 13))}:00`;

const bucketLabel = (key, bucket, multiDayHour = false, showYear = false) => {
  if (!key) return '';
  if (bucket === 'hour') {
    return multiDayHour ? dateLabel(key, showYear) : hourLabel(key);
  }
  return dateLabel(key, showYear);
};

const bucketTooltipLabel = (key, bucket) => {
  if (!key) return '-';
  if (bucket === 'hour') {
    return `${fullDateLabel(key)} ${hourLabel(key)}`;
  }
  return fullDateLabel(key);
};

const slotForIndex = (index, count, slots) => {
  if (count <= 1 || slots <= 1) return 0;
  return Math.round((index * (slots - 1)) / (count - 1));
};

const pickEvenlySpaced = (entries, limit) => {
  if (entries.length <= limit) return entries;
  const selected = [];
  const used = new Set();
  for (let index = 0; index < limit; index += 1) {
    const sourceIndex = Math.round((index * (entries.length - 1)) / (limit - 1));
    if (used.has(sourceIndex)) continue;
    selected.push(entries[sourceIndex]);
    used.add(sourceIndex);
  }
  return selected;
};

const buildBucketLabels = (positionedItems, bucket, rows, columns) => {
  const dateCount = new Set(positionedItems.map(({ item }) => String(item?.date || '').slice(0, 10)).filter(Boolean)).size;
  const showYear = new Set(positionedItems.map(({ item }) => String(item?.date || '').slice(0, 4)).filter(Boolean)).size > 1;
  const multiDayHour = bucket === 'hour' && dateCount > 1;
  const entries = positionedItems.map((entry, sourceIndex) => ({ ...entry, sourceIndex }));
  const candidates = multiDayHour
    ? entries.filter(({ item }, index) => {
      const dateKey = String(item?.date || '').slice(0, 10);
      return dateKey && entries.findIndex(({ item: entryItem }) => String(entryItem?.date || '').slice(0, 10) === dateKey) === index;
    })
    : entries;
  const limit = bucket === 'hour' && !multiDayHour ? HEATMAP_AXIS_HOUR_LABELS : HEATMAP_AXIS_DATE_LABELS;

  return pickEvenlySpaced(candidates, limit).map(({ item, slot, sourceIndex }) => {
    const key = item?.date || '';
    const column = rows === 1 ? slot : Math.floor(slot / rows);
    const leftPercent = columns <= 1 ? 0 : (column / (columns - 1)) * 100;
    return {
      key: `${key}-${sourceIndex}`,
      label: bucketLabel(key, bucket, multiDayHour, showYear),
      leftPercent
    };
  });
};

const TokenUsageHeatmapPanel = ({ isOpen, onClose, queryParams = {} }) => {
  const [items, setItems] = useState([]);
  const [loading, setLoading] = useState(false);
  const [refreshing, setRefreshing] = useState(false);
  const [isRendered, setIsRendered] = useState(isOpen);
  const [dropdownState, setDropdownState] = useState('');
  const [horizontalOffset, setHorizontalOffset] = useState(0);
  const panelRef = useRef(null);
  const toast = useToast();
  const queryKey = useMemo(() => JSON.stringify(queryParams || {}), [queryParams]);

  useEffect(() => {
    let frameId;
    let closeTimerId;

    if (isOpen) {
      if (!isRendered) {
        setIsRendered(true);
        return undefined;
      }
      frameId = window.requestAnimationFrame(() => setDropdownState('is-open'));
    } else if (isRendered) {
      setDropdownState('is-closing');
      const closeDuration = parseFloat(
        getComputedStyle(document.documentElement).getPropertyValue('--dropdown-close-dur')
      ) || 150;
      closeTimerId = window.setTimeout(() => {
        setIsRendered(false);
        setDropdownState('');
      }, closeDuration);
    }

    return () => {
      if (frameId) window.cancelAnimationFrame(frameId);
      if (closeTimerId) window.clearTimeout(closeTimerId);
    };
  }, [isOpen, isRendered]);

  useEffect(() => {
    if (!isRendered) return undefined;

    const updatePosition = () => {
      const panel = panelRef.current;
      const anchor = panel?.offsetParent;
      if (!panel || !anchor) return;

      const viewportMargin = 16;
      const anchorRight = anchor.getBoundingClientRect().right;
      const panelLeft = anchorRight - panel.offsetWidth;
      let nextOffset = 0;

      if (panelLeft < viewportMargin) {
        nextOffset = viewportMargin - panelLeft;
      } else if (anchorRight > window.innerWidth - viewportMargin) {
        nextOffset = window.innerWidth - viewportMargin - anchorRight;
      }

      setHorizontalOffset(nextOffset);
    };

    const frameId = window.requestAnimationFrame(updatePosition);
    window.addEventListener('resize', updatePosition);
    return () => {
      window.cancelAnimationFrame(frameId);
      window.removeEventListener('resize', updatePosition);
    };
  }, [isRendered]);

  const loadData = useCallback(async (showRefreshing = false) => {
    if (showRefreshing) {
      setRefreshing(true);
    } else {
      setLoading(true);
    }
    try {
      const data = await fetchTokenUsageHeatmapData(queryParams);
      setItems(Array.isArray(data) ? data : []);
    } catch (error) {
      console.error('加载 Token 活动分布失败:', error);
      toast.error(error?.message || 'Token 活动分布加载失败', 'Token 活动分布加载失败');
    } finally {
      setLoading(false);
      setRefreshing(false);
    }
  }, [queryKey, toast]); // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => {
    if (!isOpen) return;
    loadData(false);
  }, [isOpen, loadData]);

  const bucket = useMemo(() => getBucketKind(items), [items]);
  const gridRows = HEATMAP_ROWS;
  const gridColumns = Math.max(HEATMAP_MIN_COLUMNS, Math.ceil(items.length / gridRows));
  const heatmapSlots = gridRows * gridColumns;
  const positionedItems = useMemo(
    () => items.map((item, index) => ({
      item,
      slot: slotForIndex(index, items.length, heatmapSlots)
    })),
    [items, heatmapSlots]
  );
  const displayItems = useMemo(
    () => {
      const slots = Array.from({ length: heatmapSlots }, () => null);
      positionedItems.forEach(({ item, slot }) => {
        slots[slot] = item;
      });
      return slots;
    },
    [positionedItems, heatmapSlots]
  );
  const bucketLabels = useMemo(
    () => buildBucketLabels(positionedItems, bucket, gridRows, gridColumns),
    [positionedItems, bucket, gridRows, gridColumns]
  );
  const total = items.reduce((sum, item) => sum + Number(item.total || 0), 0);
  const activeBuckets = items.filter(item => Number(item.total || 0) > 0 || Number(item.requestCount || 0) > 0).length;
  const activeUnit = bucket === 'hour' ? '小时' : '天';
  const peak = items.reduce((max, item) => Math.max(max, Number(item.total || 0)), 0);

  if (!isRendered) return null;

  return (
    <div
      id="token-usage-heatmap-panel"
      ref={panelRef}
      data-origin="top-right"
      aria-hidden={!isOpen}
      className={`t-dropdown absolute top-12 right-0 z-30 w-[min(780px,calc(100vw-2rem))] rounded-xl border border-slate-200 bg-white shadow-xl ring-1 ring-black/5 ${dropdownState}`}
      style={{ translate: `${horizontalOffset}px 0` }}
    >
      <div className="flex items-start justify-between gap-4 rounded-t-xl border-b border-slate-100 bg-slate-50/70 px-4 py-3">
        <div className="min-w-0">
          <div className="flex items-center gap-2 text-sm font-semibold text-slate-900">
            <span className="flex h-7 w-7 items-center justify-center rounded-lg bg-indigo-50 text-indigo-600 ring-1 ring-indigo-100">
              <Activity className="h-4 w-4" />
            </span>
            <span>Token 活动分布</span>
          </div>
          <p className="mt-1 pl-9 text-xs text-slate-500">按{bucket === 'hour' ? '小时' : '天'}聚合，颜色越深表示使用越高</p>
        </div>
        <div className="flex shrink-0 items-center gap-2">
          <Tooltip content="刷新分布">
            <button
              type="button"
              onClick={() => loadData(true)}
              disabled={refreshing || loading}
              className="motion-control rounded-md p-1.5 text-slate-400 hover:bg-white hover:text-slate-700 disabled:opacity-50"
              aria-label="刷新分布"
            >
              <RefreshCw className={`h-4 w-4 ${refreshing ? 'animate-spin' : ''}`} />
            </button>
          </Tooltip>
          <button
            type="button"
            onClick={onClose}
            className="motion-control rounded-md p-1.5 text-slate-400 hover:bg-white hover:text-slate-700"
            aria-label="关闭分布"
          >
            <X className="h-4 w-4" />
          </button>
        </div>
      </div>

      <div className="grid grid-cols-3 divide-x divide-slate-100 border-b border-slate-100 bg-white">
        <div className="min-w-0 px-4 py-2.5">
          <div className="text-[10px] font-semibold uppercase text-slate-400">活跃</div>
          <div className="truncate text-sm font-semibold tabular-nums text-emerald-600">{activeBuckets} {activeUnit}</div>
        </div>
        <div className="min-w-0 px-4 py-2.5">
          <div className="text-[10px] font-semibold uppercase text-slate-400">总 Token</div>
          <div className="truncate text-sm font-semibold tabular-nums text-indigo-600">{formatTokens(total)}</div>
        </div>
        <div className="min-w-0 px-4 py-2.5">
          <div className="text-[10px] font-semibold uppercase text-slate-400">峰值</div>
          <div className="truncate text-sm font-semibold tabular-nums text-amber-600">{formatTokens(peak)}</div>
        </div>
      </div>

      <div className="px-4 py-4">
        <div
          className={`t-skel h-[126px] ${loading ? 'is-resetting' : 'is-revealed'}`}
          aria-busy={loading}
        >
          <div className={`t-skel-skeleton ${loading ? 'is-pulsing' : ''}`} aria-hidden="true">
            <div
              className="grid grid-flow-col"
              style={{
                gridTemplateColumns: `repeat(${gridColumns}, minmax(0, 1fr))`,
                gridTemplateRows: `repeat(${gridRows}, ${HEATMAP_CELL_SIZE}px)`,
                gap: `${HEATMAP_CELL_GAP}px`
              }}
            >
              {displayItems.map((_, index) => (
                <span
                  key={`skeleton-${index}`}
                  className="block rounded-[3px] border border-slate-100 bg-slate-100"
                  style={{ height: `${HEATMAP_CELL_SIZE}px` }}
                />
              ))}
            </div>
            <div className="mt-2 flex h-4 items-start justify-between">
              {[32, 40, 36, 44].map((width) => (
                <span key={width} className="h-2 rounded bg-slate-100" style={{ width: `${width}px` }} />
              ))}
            </div>
          </div>

          <div
            className={`t-skel-content ${loading ? 'pointer-events-none' : 'pointer-events-auto'}`}
            aria-hidden={loading}
          >
            <div className="w-full">
              <div
                className="grid grid-flow-col"
                role="img"
                aria-label={`Token 活动分布，共 ${activeBuckets} 个活跃${activeUnit}，总计 ${formatTokens(total)} Token`}
                style={{
                  gridTemplateColumns: `repeat(${gridColumns}, minmax(0, 1fr))`,
                  gridTemplateRows: `repeat(${gridRows}, ${HEATMAP_CELL_SIZE}px)`,
                  gap: `${HEATMAP_CELL_GAP}px`
                }}
              >
                {displayItems.map((item, index) => {
                  if (!item) {
                    return (
                      <span
                        key={`empty-${index}`}
                        className={`block rounded-[3px] border ${HEAT_LEVELS[0]}`}
                        style={{ width: '100%', height: `${HEATMAP_CELL_SIZE}px` }}
                        aria-hidden="true"
                      />
                    );
                  }
                  const key = item?.date || String(index);
                  const level = heatLevel(item);
                  return (
                    <Tooltip
                      key={`${key}-${index}`}
                      content={`${bucketTooltipLabel(item?.date, bucket)} · ${formatTokens(item?.total || 0)} tokens · ${item?.requestCount || 0} 请求`}
                      className="flex w-full"
                    >
                      <span
                        className={`heatmap-cell block rounded-[3px] border ${HEAT_LEVELS[level]}`}
                        style={{ width: '100%', height: `${HEATMAP_CELL_SIZE}px` }}
                        aria-hidden="true"
                      />
                    </Tooltip>
                  );
                })}
              </div>
              <div
                className="relative mt-2 h-4 text-[11px] leading-none text-slate-500"
              >
                {bucketLabels.map((label) => {
                  const transform = label.leftPercent >= 99
                    ? 'translateX(-100%)'
                    : label.leftPercent > 0
                      ? 'translateX(-50%)'
                      : undefined;
                  return (
                    <span
                      key={label.key}
                      className="absolute top-0 whitespace-nowrap"
                      style={{ left: `${label.leftPercent}%`, transform }}
                    >
                      {label.label}
                    </span>
                  );
                })}
              </div>
            </div>
          </div>
          {loading && <span className="sr-only">Token 活动分布加载中</span>}
        </div>

        <div className="mt-4 flex items-center justify-between border-t border-slate-100 pt-3 text-xs text-slate-500">
          <div className="flex items-center gap-2">
            <span>低</span>
            <div className="flex gap-1">
              {HEAT_LEVELS.map((level, index) => (
                <span key={level} className={`h-2.5 w-2.5 rounded-[3px] border ${HEAT_LEVELS[index]}`} />
              ))}
            </div>
            <span>高</span>
          </div>
          <div className="tabular-nums">{items.length} 个采样点</div>
        </div>
      </div>

      <div className="absolute -top-1.5 right-20 h-3 w-3 rotate-45 border-l border-t border-slate-200 bg-slate-50" />
    </div>
  );
};

export default TokenUsageHeatmapPanel;
