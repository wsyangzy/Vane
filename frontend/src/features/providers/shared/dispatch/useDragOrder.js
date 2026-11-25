import { useId, useRef, useState } from 'react';

export default function useDragOrder(keys, onMove, disabled = false) {
  const scope = useId();
  const active = useRef(null);
  const [dragged, setDragged] = useState(null);
  const [over, setOver] = useState(null);
  const clear = () => { active.current = null; setDragged(null); setOver(null); };
  const accepts = event => !disabled && active.current !== null && event.dataTransfer.types.includes('application/x-vane-order');
  return {
    handleProps: key => ({
      draggable: !disabled && keys.length > 1,
      disabled: disabled || keys.length < 2,
      onDragStart: event => {
        event.stopPropagation();
        active.current = key;
        setDragged(key);
        event.dataTransfer.effectAllowed = 'move';
        event.dataTransfer.setData('application/x-vane-order', scope);
      },
      onDragEnd: event => { event.stopPropagation(); clear(); },
      onKeyDown: event => {
        const index = keys.indexOf(key);
        const next = { ArrowUp: index - 1, ArrowDown: index + 1, Home: 0, End: keys.length - 1 }[event.key];
        if (next == null || disabled) return;
        event.preventDefault();
        event.stopPropagation();
        if (next >= 0 && next < keys.length && next !== index) onMove(key, keys[next]);
      },
    }),
    rowProps: key => ({
      'data-order-dragging': dragged === key,
      'data-order-over': over === key && dragged !== key,
      onDragOver: event => {
        if (!accepts(event)) return;
        event.preventDefault();
        event.stopPropagation();
        event.dataTransfer.dropEffect = 'move';
        setOver(key);
      },
      onDragLeave: event => { if (!event.currentTarget.contains(event.relatedTarget)) setOver(previous => previous === key ? null : previous); },
      onDrop: event => {
        if (!accepts(event) || event.dataTransfer.getData('application/x-vane-order') !== scope) return;
        event.preventDefault();
        event.stopPropagation();
        const from = active.current;
        clear();
        if (from !== key && keys.includes(from)) onMove(from, key);
      },
    }),
  };
}
