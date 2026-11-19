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
  const [display, setDisplay] = useState(() => live && target != null ? 0 : target || 0);
  const current = useRef(display);
  const reducedMotion = typeof window !== 'undefined'
    && window.matchMedia?.('(prefers-reduced-motion: reduce)').matches === true;
  const animate = live && !reducedMotion;

  useEffect(() => {
    if (target == null) return undefined;
    if (!animate) {
      current.current = target;
      return undefined;
    }

    const from = current.current;
    if (from === target) return undefined;
    const startedAt = performance.now();
    let frame = 0;
    const tick = now => {
      const progress = Math.min(1, (now - startedAt) / 380);
      const eased = 1 - ((1 - progress) ** 3);
      const next = from + (target - from) * eased;
      current.current = next;
      setDisplay(next);
      if (progress < 1) frame = requestAnimationFrame(tick);
    };
    frame = requestAnimationFrame(tick);
    return () => cancelAnimationFrame(frame);
  }, [target, animate]);

  return <span className={className} aria-live="off">
    {target == null ? '—' : formatValue(animate ? display : target, format)}
  </span>;
});
