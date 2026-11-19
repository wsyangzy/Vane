import { useId, useLayoutEffect, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import { ChevronDown, X } from 'lucide-react';
import { usePresence } from '@/shared/hooks/usePresence.js';

const LongContextPricing = ({ pricing, fields, fieldMapping, modelName, speedLabel }) => {
  const [open, setOpen] = useState(false);
  const presence = usePresence(open, 'dropdown');
  const panelId = useId();
  const triggerRef = useRef(null);
  const panelRef = useRef(null);

  useLayoutEffect(() => {
    if (!open) return undefined;
    const trigger = triggerRef.current;
    const panel = panelRef.current;
    const anchor = trigger.getBoundingClientRect();
    // Resolve the first origin before animating; reopening during exit keeps
    // the current transition so opacity does not jump back to zero.
    const firstPlacement = !panel.dataset.origin;
    if (firstPlacement) panel.style.transition = 'none';
    const margin = 8;
    const gap = 6;
    const width = Math.min(Math.max(anchor.width, 280), window.innerWidth - margin * 2);
    panel.style.width = `${width}px`;
    panel.style.maxHeight = '';
    // Measure layout size before the entrance scale is applied visually.
    const height = panel.offsetHeight;
    const below = Math.max(0, window.innerHeight - anchor.bottom - margin - gap);
    const above = Math.max(0, anchor.top - margin - gap);
    const upward = below < height && above > below;
    panel.dataset.origin = upward ? 'bottom-left' : 'top-left';
    const maxHeight = upward ? above : below;
    panel.style.maxHeight = `${maxHeight}px`;
    panel.style.left = `${Math.max(margin, Math.min(anchor.left, window.innerWidth - width - margin))}px`;
    panel.style.top = `${upward ? anchor.top - Math.min(height, maxHeight) - gap : anchor.bottom + gap}px`;
    if (firstPlacement) {
      void panel.offsetHeight;
      panel.style.removeProperty('transition');
    }
    panel.focus({ preventScroll: true });

    const dismiss = (event) => {
      if (!trigger.contains(event.target) && !panel.contains(event.target)) setOpen(false);
    };
    const onKeyDown = (event) => {
      if (event.key !== 'Escape') return;
      event.preventDefault();
      event.stopPropagation();
      setOpen(false);
      trigger.focus({ preventScroll: true });
    };
    const onScroll = (event) => {
      if (panel.contains(event.target)) return;
      setOpen(false);
      if (panel.contains(document.activeElement)) trigger.focus({ preventScroll: true });
    };
    const onResize = () => {
      setOpen(false);
      if (panel.contains(document.activeElement)) trigger.focus({ preventScroll: true });
    };
    document.addEventListener('pointerdown', dismiss);
    document.addEventListener('focusin', dismiss);
    document.addEventListener('keydown', onKeyDown);
    window.addEventListener('scroll', onScroll, true);
    window.addEventListener('resize', onResize);
    return () => {
      document.removeEventListener('pointerdown', dismiss);
      document.removeEventListener('focusin', dismiss);
      document.removeEventListener('keydown', onKeyDown);
      window.removeEventListener('scroll', onScroll, true);
      window.removeEventListener('resize', onResize);
    };
  }, [open]);

  return <>
    <button ref={triggerRef} type="button" className="pricing-long-context" aria-expanded={open} aria-controls={presence.present ? panelId : undefined} aria-haspopup="dialog" onClick={() => setOpen((value) => !value)}>
      <span>长上下文单价</span><span>&gt; {pricing.longContextThreshold.toLocaleString()} Token<ChevronDown size={12} aria-hidden="true" /></span>
    </button>
    {presence.present && createPortal(
      <div ref={panelRef} id={panelId} role="dialog" aria-labelledby={`${panelId}-title`} aria-describedby={`${panelId}-context`} aria-hidden={presence.closing} inert={presence.closing} tabIndex={-1} className={`pricing-context-popover t-dropdown ${presence.className}`}>
        <header className="pricing-context-popover-header">
          <div><h4 id={`${panelId}-title`}>{modelName} · 长上下文单价</h4><p id={`${panelId}-context`}>{speedLabel} · &gt; {pricing.longContextThreshold.toLocaleString()} Token · USD / 100 万 Token</p></div>
          <button type="button" aria-label="关闭长上下文单价" onClick={() => { setOpen(false); triggerRef.current.focus({ preventScroll: true }); }}><X size={14} /></button>
        </header>
        <dl className="pricing-rate-list">{fields.map(({ field, label }) => <div key={field} data-field={field}><dt>{label}</dt><dd><span>$</span>{pricing[fieldMapping[field]] ?? '—'}</dd></div>)}</dl>
      </div>, document.body
    )}
  </>;
};

export default LongContextPricing;
