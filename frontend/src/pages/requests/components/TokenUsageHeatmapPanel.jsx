// ============================================
// TokenUsageHeatmapPanel - 请求追踪 Token 活动分布
// ============================================

import { useCallback, useEffect, useMemo, useState } from 'react';
import { Activity, RefreshCw, X } from 'lucide-react';
import { Tooltip } from '@components/ui';
import { fetchTokenUsageHeatmapData } from '@utils/api.js';

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
  const queryKey = useMemo(() => JSON.stringify(queryParams || {}), [queryParams]);

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
      setItems([]);
    } finally {
      setLoading(false);
      setRefreshing(false);
    }
  }, [queryKey]); // eslint-disable-line react-hooks/exhaustive-deps

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

  if (!isOpen) return null;

  return (
    <div className="absolute top-12 right-0 z-30 w-[min(780px,calc(100vw-2rem))] rounded-xl border border-gray-100 bg-white shadow-xl ring-1 ring-black/5 animate-in fade-in zoom-in-95 slide-in-from-top-2 duration-200 origin-top-right">
      <div className="flex items-start justify-between gap-4 border-b border-slate-100 px-4 py-3">
        <div className="min-w-0">
          <div className="flex items-center gap-2 text-sm font-semibold text-slate-900">
            <Activity className="h-4 w-4 text-indigo-500" />
            <span>Token 活动分布</span>
          </div>
          <p className="mt-1 text-xs text-slate-500">按{bucket === 'hour' ? '小时' : '天'}聚合，颜色越深表示使用越高</p>
        </div>
        <div className="flex shrink-0 items-center gap-2">
          <div className="hidden text-right text-xs text-slate-500 sm:block">
            <span className="font-semibold text-slate-700">{activeBuckets}</span> {activeUnit}活跃 · {formatTokens(total)}
          </div>
          <Tooltip content="刷新分布">
            <button
              type="button"
              onClick={() => loadData(true)}
              disabled={refreshing || loading}
              className="rounded-md p-1.5 text-slate-400 transition-colors hover:bg-slate-100 hover:text-slate-700 disabled:opacity-50"
              aria-label="刷新分布"
            >
              <RefreshCw className={`h-4 w-4 ${refreshing ? 'animate-spin' : ''}`} />
            </button>
          </Tooltip>
          <button
            type="button"
            onClick={onClose}
            className="rounded-md p-1.5 text-slate-400 transition-colors hover:bg-slate-100 hover:text-slate-700"
            aria-label="关闭分布"
          >
            <X className="h-4 w-4" />
          </button>
        </div>
      </div>

      <div className="px-4 py-4">
        {loading ? (
          <div className="flex h-32 items-center justify-center text-sm text-slate-400">
            <RefreshCw className="mr-2 h-4 w-4 animate-spin" />
            加载中...
          </div>
        ) : (
          <div className="pb-1">
            <div className="w-full">
              <div
                className="grid grid-flow-col"
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
                        className={`block rounded-[3px] border ${HEAT_LEVELS[level]}`}
                        style={{ width: '100%', height: `${HEATMAP_CELL_SIZE}px` }}
                        aria-label={`${bucketTooltipLabel(item?.date, bucket)} ${formatTokens(item?.total || 0)} tokens`}
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
        )}

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
          <div className="tabular-nums">峰值 {formatTokens(peak)}</div>
        </div>
      </div>

      <div className="absolute -top-1.5 right-20 h-3 w-3 rotate-45 border-l border-t border-gray-100 bg-white" />
    </div>
  );
};

export default TokenUsageHeatmapPanel;
