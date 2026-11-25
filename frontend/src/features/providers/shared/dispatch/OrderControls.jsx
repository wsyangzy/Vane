import { useRef, useState } from 'react';
import { GripVertical } from 'lucide-react';
import Tooltip from '@/shared/ui/Tooltip.jsx';
import './profileEditor.css';

export function OrderInput({ value, label, prefix, min, max, disabled, onCommit }) {
  const [pending, setPending] = useState(null);
  const cancelled = useRef(false);
  const commit = () => {
    const number = pending === null || !pending.trim() ? NaN : Number(pending);
    if (!cancelled.current && Number.isSafeInteger(number) && (min == null || number >= min) && (max == null || number <= max)) onCommit(number);
    cancelled.current = false;
    setPending(null);
  };
  const displayValue = pending ?? (min === 1 ? String(value).padStart(2, '0') : value);
  return <Tooltip asChild content={`${label}：输入数字后按 Enter 或离开输入框确认`}>
    <span className="dispatch-order-control" data-prefixed={Boolean(prefix)} data-disabled={Boolean(disabled)}>
    {prefix && <span className="dispatch-order-caption">{prefix}</span>}
    <input type="number" className="dispatch-order-input" style={{ width: `${Math.max(2, Math.min(10, String(displayValue).length))}ch` }} aria-label={label} value={displayValue} min={min} max={max} step="1" disabled={disabled} onChange={event => setPending(event.target.value)} onFocus={event => event.target.select()} onBlur={commit} onKeyDown={event => {
      if (event.key !== 'Enter' && event.key !== 'Escape') return;
      event.preventDefault();
      event.stopPropagation();
      cancelled.current = event.key === 'Escape';
      event.currentTarget.blur();
    }} />
    </span>
  </Tooltip>;
}

export function DragHandle({ label, ...props }) {
  return <Tooltip asChild content="拖动排序；也可用 ↑ / ↓ 或 Home / End 调整"><button type="button" className="dispatch-drag-handle" aria-label={label} {...props}><GripVertical size={15} /></button></Tooltip>;
}
