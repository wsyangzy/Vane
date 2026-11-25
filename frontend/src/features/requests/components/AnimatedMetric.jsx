import { memo, useEffect, useRef, useState } from 'react';
import { formatDuration } from '@/services/http/api.js';
import { formatRequestInsightCost } from '../utils/requestInsights.js';

const integer = new Intl.NumberFormat('en-US', { maximumFractionDigits: 0 });
const compact = new Intl.NumberFormat('zh-CN', { notation: 'compact', maximumFractionDigits: 1 });
const percent = new Intl.NumberFormat('zh-CN', { style: 'percent', maximumFractionDigits: 1 });

function formatValue(value, format) {
  if (format === 'cost') return formatRequestInsightCost(value);
  if (format === 'duration') return formatDuration(Math.round(value));
  if (format === 'percent') return percent.format(value);
  if (format === 'compact') return compact.format(value);
  return integer.format(Math.round(value));
}

export default memo(function AnimatedMetric({ value, live = false, format = 'number', className }) {
  const target = value == null || !Number.isFinite(Number(value)) ? null : Number(value);
  const reducedMotion = typeof window !== 'undefined'
    && window.matchMedia?.('(prefers-reduced-motion: reduce)').matches === true;
  const animate = Boolean(live) && !reducedMotion;
  const waiting = live === 'waiting' && !reducedMotion;
  const [playback, setPlayback] = useState(() => ({ waiting, display: animate ? 0 : target || 0 }));
  const current = useRef(playback.display);
  if (playback.waiting !== waiting) {
    setPlayback({ waiting, display: waiting ? 0 : playback.display });
  }

  useEffect(() => {
    if (waiting) {
      current.current = 0;
      return undefined;
    }
    if (target == null) return undefined;
    if (!animate) {
      current.current = target;
      return undefined;
    }

    const from = current.current;
    if (from === target) return undefined;
    let startedAt;
    let frame = 0;
    const tick = now => {
      startedAt ??= now;
      const progress = Math.max(0, Math.min(1, (now - startedAt) / 360));
      const eased = progress * (2 - progress);
      const next = from + (target - from) * eased;
      current.current = next;
      setPlayback({ waiting: false, display: next });
      if (progress < 1) frame = requestAnimationFrame(tick);
    };
    frame = requestAnimationFrame(tick);
    return () => cancelAnimationFrame(frame);
  }, [target, animate, waiting]);

  return <span className={className} aria-live="off">
    {target == null ? '—' : formatValue(waiting ? 0 : animate ? playback.display : target, format)}
  </span>;
});
