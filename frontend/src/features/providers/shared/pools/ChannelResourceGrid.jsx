import { Children, useCallback, useEffect, useRef, useState } from 'react';
import { moveChannelCard, reconcileChannelCardOrder } from './channelCardOrder.js';
import './channelWorkspace.css';

const interactiveSelector = 'button, a, input, textarea, select, [role="button"], [role="switch"], [contenteditable="true"]';

const scrollContainer = (element) => {
  for (let parent = element.parentElement; parent; parent = parent.parentElement) {
    if (/(auto|scroll)/.test(getComputedStyle(parent).overflowY)) return parent;
  }
  return document.scrollingElement;
};

const dropTarget = (grid, x, y) => {
  const bounds = grid.getBoundingClientRect();
  if (x < bounds.left || x > bounds.right || y < bounds.top || y > bounds.bottom || y < 0 || y > window.innerHeight) return null;
  const scroll = scrollContainer(grid).getBoundingClientRect();
  if (x < scroll.left || x > scroll.right || y < scroll.top || y > scroll.bottom) return null;
  let nearest = null;
  let distance = Infinity;
  for (const slot of grid.children) {
    const rect = slot.getBoundingClientRect();
    const nextDistance = Math.hypot(x - rect.left - rect.width / 2, y - rect.top - rect.height / 2);
    if (nextDistance < distance) {
      nearest = slot.dataset.channelResourceSlot;
      distance = nextDistance;
    }
  }
  return nearest;
};

export default function ChannelResourceGrid({ provider, label, children }) {
  const storageKey = `vane.channel-resource-order.${provider}`;
  const [savedOrder, setSavedOrder] = useState(() => {
    try { return JSON.parse(localStorage.getItem(storageKey)); } catch { return []; }
  });
  const [dragging, setDragging] = useState(null);
  const gridRef = useRef(null);
  const dragRef = useRef(null);
  const frameRef = useRef(0);
  const suppressClick = useRef(false);
  const clickTimerRef = useRef(0);
  const items = Children.toArray(children);
  const keyedItems = new Map(items.map((item) => [item.key, item]));
  const orderedKeys = reconcileChannelCardOrder([...keyedItems.keys()], savedOrder);
  const suppressNextClicks = useCallback(() => {
    suppressClick.current = true;
    clearTimeout(clickTimerRef.current);
    clickTimerRef.current = setTimeout(() => { suppressClick.current = false; }, 350);
  }, []);

  const finishDrag = (cancelled = false) => {
    cancelAnimationFrame(frameRef.current);
    const drag = dragRef.current;
    dragRef.current = null;
    setDragging(null);
    const grid = gridRef.current;
    if (!drag || !grid) return;
    if (drag.active) {
      suppressNextClicks();
      if (!cancelled) {
        const target = dropTarget(grid, drag.x, drag.y);
        const nextOrder = moveChannelCard(orderedKeys, drag.key, target);
        if (nextOrder !== orderedKeys) {
          setSavedOrder(nextOrder);
          try { localStorage.setItem(storageKey, JSON.stringify(nextOrder)); } catch { /* Keep the order for this visit. */ }
        }
      }
    }
    if (drag.capture.hasPointerCapture(drag.pointerId)) drag.capture.releasePointerCapture(drag.pointerId);
  };

  useEffect(() => {
    const cancel = () => {
      cancelAnimationFrame(frameRef.current);
      const drag = dragRef.current;
      dragRef.current = null;
      setDragging(null);
      if (drag?.active) suppressNextClicks();
      if (drag?.capture.hasPointerCapture(drag.pointerId)) drag.capture.releasePointerCapture(drag.pointerId);
    };
    const cancelOnEscape = (event) => { if (event.key === 'Escape') cancel(); };
    window.addEventListener('blur', cancel);
    window.addEventListener('resize', cancel);
    window.addEventListener('keydown', cancelOnEscape);
    return () => {
      window.removeEventListener('blur', cancel);
      window.removeEventListener('resize', cancel);
      window.removeEventListener('keydown', cancelOnEscape);
      cancelAnimationFrame(frameRef.current);
      clearTimeout(clickTimerRef.current);
      const drag = dragRef.current;
      dragRef.current = null;
      if (drag?.capture.hasPointerCapture(drag.pointerId)) drag.capture.releasePointerCapture(drag.pointerId);
    };
  }, [suppressNextClicks]);

  const paintDrag = () => {
    const drag = dragRef.current;
    const grid = gridRef.current;
    if (!drag?.active || !grid) return;
    const slot = [...grid.children].find((element) => element.dataset.channelResourceSlot === drag.key);
    if (!slot) { finishDrag(true); return; }
    const scroll = drag.scroll;
    const bounds = scroll === document.scrollingElement ? { top: 0, bottom: window.innerHeight } : scroll.getBoundingClientRect();
    if (drag.y < bounds.top + 36) scroll.scrollTop -= Math.min(14, (bounds.top + 36 - drag.y) / 3);
    else if (drag.y > bounds.bottom - 36) scroll.scrollTop += Math.min(14, (drag.y - bounds.bottom + 36) / 3);
    const rect = slot.getBoundingClientRect();
    const gridBounds = grid.getBoundingClientRect();
    // Keep the floating card from creating extra scrollable space at the edges.
    const next = {
      key: drag.key,
      x: Math.max(gridBounds.left, Math.min(drag.x - drag.startX + drag.rect.left, gridBounds.right - rect.width)) - rect.left,
      y: Math.max(gridBounds.top, Math.min(drag.y - drag.startY + drag.rect.top, gridBounds.bottom - rect.height)) - rect.top,
      target: dropTarget(grid, drag.x, drag.y)
    };
    setDragging((current) => current?.key === next.key && current.x === next.x && current.y === next.y && current.target === next.target ? current : next);
    frameRef.current = requestAnimationFrame(paintDrag);
  };

  const startDrag = (event) => {
    if (event.button !== 0 || !event.isPrimary || event.target.closest(interactiveSelector)) return;
    const slot = event.target.closest('[data-channel-resource-slot]');
    if (!slot || slot.parentElement !== event.currentTarget) return;
    // Capture on the original target so double-clicks still reach the card.
    event.target.setPointerCapture(event.pointerId);
    dragRef.current = {
      pointerId: event.pointerId, capture: event.target, key: slot.dataset.channelResourceSlot,
      startX: event.clientX, startY: event.clientY, x: event.clientX, y: event.clientY,
      rect: slot.getBoundingClientRect(), scroll: scrollContainer(event.currentTarget), active: false
    };
  };

  const moveDrag = (event) => {
    const drag = dragRef.current;
    if (!drag || event.pointerId !== drag.pointerId) return;
    drag.x = event.clientX;
    drag.y = event.clientY;
    if (!drag.active && Math.hypot(drag.x - drag.startX, drag.y - drag.startY) >= 6) {
      drag.active = true;
      frameRef.current = requestAnimationFrame(paintDrag);
    }
    if (drag.active) event.preventDefault();
  };

  const suppressDragClick = (event) => {
    if (suppressClick.current) {
      event.preventDefault();
      event.stopPropagation();
    }
  };

  return <div
    ref={gridRef} aria-label={label} className={`channel-resource-grid ${dragging ? 'is-dragging' : ''}`}
    onPointerDown={startDrag} onPointerMove={moveDrag}
    onPointerUp={(event) => {
      if (dragRef.current?.pointerId !== event.pointerId) return;
      dragRef.current.x = event.clientX;
      dragRef.current.y = event.clientY;
      finishDrag();
    }}
    onPointerCancel={() => finishDrag(true)} onLostPointerCapture={() => finishDrag(true)}
    onClickCapture={suppressDragClick} onDoubleClickCapture={suppressDragClick}
  >
    {orderedKeys.map((key) => <div key={key} data-channel-resource-slot={key}
      data-dragging={dragging?.key === key || undefined}
      data-drop-target={dragging?.target === key && dragging.key !== key || undefined}
      className="channel-resource-slot"
    ><div className="channel-resource-movable" style={dragging?.key === key ? { transform: `translate3d(${dragging.x}px, ${dragging.y}px, 0)` } : undefined}>{keyedItems.get(key)}</div></div>)}
  </div>;
}
