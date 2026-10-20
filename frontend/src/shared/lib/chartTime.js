// ============================================
// 图表时间轴工具
// 统一处理后端 timestamp（RFC3339）与时间窗口填充
// ============================================

export const parseTimestampMs = (value) => {
  if (!value) return null;
  if (typeof value === 'number' && Number.isFinite(value)) return value;

  if (typeof value === 'string') {
    // unix ms
    if (/^\d+$/.test(value)) {
      const n = Number(value);
      return Number.isFinite(n) ? n : null;
    }
    const d = new Date(value);
    const ms = d.getTime();
    return Number.isFinite(ms) ? ms : null;
  }
  return null;
};

export const formatHHMM = (timestampOrMs) => {
  const ms = parseTimestampMs(timestampOrMs);
  if (ms == null) return String(timestampOrMs ?? '');
  const d = new Date(ms);
  const hh = String(d.getHours()).padStart(2, '0');
  const mm = String(d.getMinutes()).padStart(2, '0');
  return `${hh}:${mm}`;
};

/**
 * 将时间序列填充到指定的分钟窗口。
 * - 输入数据需要包含 timestamp（RFC3339 或 unix ms）字段；否则原样返回
 * - 会对齐到 stepMs 的桶，并补齐缺失桶（值使用 defaults）
 */
export const padTimeSeries = (data, { minutes, stepMs, defaults }) => {
  if (!Array.isArray(data) || data.length === 0) return [];
  if (!minutes || minutes <= 0) return data;

  const hasTimestamp = data.some(p => parseTimestampMs(p?.timestamp) != null);
  if (!hasTimestamp) return data;

  const nowMs = Date.now();
  const startMs = nowMs - minutes * 60 * 1000;
  const startBucket = Math.floor(startMs / stepMs) * stepMs;
  const endBucket = Math.floor(nowMs / stepMs) * stepMs;

  const buckets = new Map();
  for (const point of data) {
    const tsMs = parseTimestampMs(point?.timestamp);
    if (tsMs == null) continue;
    const bucketMs = Math.floor(tsMs / stepMs) * stepMs;
    buckets.set(bucketMs, point);
  }

  const padded = [];
  for (let t = startBucket; t <= endBucket; t += stepMs) {
    const existing = buckets.get(t);
    if (existing) {
      padded.push(existing);
      continue;
    }
    padded.push({
      ...defaults,
      timestamp: new Date(t).toISOString(),
      time: formatHHMM(t),
    });
  }
  return padded;
};
