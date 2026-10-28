import { Children, cloneElement, useCallback, useEffect, useLayoutEffect, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import { usePresence } from '@/shared/hooks/usePresence.js';

let dismissActiveTooltip = null;

const TooltipTrigger = ({ children, onShow, onHide }) => {
  const child = Children.only(children);
  return cloneElement(child, {
    'data-vane-tooltip-trigger': '',
    onPointerEnter: event => { child.props.onPointerEnter?.(event); onShow(event); },
    onPointerLeave: event => { child.props.onPointerLeave?.(event); onHide(); },
    onFocus: event => { child.props.onFocus?.(event); onShow(event); },
    onBlur: event => { child.props.onBlur?.(event); onHide(); },
  });
};

const Tooltip = ({
  content,
  children,
  placement = 'top',
  disabled = false,
  asChild = false,
  className = '',
  contentClassName = ''
}) => {
  const [isVisible, setIsVisible] = useState(false);
  const presence = usePresence(isVisible, 'tooltip');
  const [position, setPosition] = useState({ top: 0, left: 0, placement });
  const triggerRef = useRef(null);
  const tooltipRef = useRef(null);

  const hasContent = content !== undefined && content !== null && content !== '';
  const hasDisplayClass = /\b(?:block|inline-block|inline-flex|flex|grid|inline-grid)\b/.test(className);

  const hideTooltip = useCallback(() => {
    setIsVisible(false);
  }, []);

  useEffect(() => () => {
    if (dismissActiveTooltip === hideTooltip) dismissActiveTooltip = null;
  }, [hideTooltip]);

  const showTooltip = useCallback((event) => {
    const trigger = event?.currentTarget || triggerRef.current;
    if (disabled || !hasContent || !trigger || event?.defaultPrevented) return;
    // A nested trigger owns its own explanation. Do not show its parent's
    // tooltip at the same time, including bubbled keyboard focus events.
    if (event?.target?.closest('[data-vane-tooltip-trigger]') !== trigger) return;
    triggerRef.current = trigger;

    // Repeating the visible label adds no information, including text that is
    // only visually clipped by ellipsis. Icon-only controls still need a label.
    if (typeof content === 'string' || typeof content === 'number') {
      const normalize = value => String(value).replace(/\s+/g, ' ').trim();
      if (normalize(content) === normalize(trigger.innerText ?? trigger.textContent ?? '')) return;
    }

    if (dismissActiveTooltip !== hideTooltip) dismissActiveTooltip?.();
    dismissActiveTooltip = hideTooltip;
    setIsVisible(true);
  }, [content, disabled, hasContent, hideTooltip]);

  // A trigger's distance from the edge cannot predict a multiline tooltip's
  // height. Measure before paint, then flip and clamp the actual popup bounds.
  useLayoutEffect(() => {
    if (!isVisible || !tooltipRef.current || !triggerRef.current) return;
    const anchor = triggerRef.current.getBoundingClientRect();
    const popup = tooltipRef.current.getBoundingClientRect();
    const margin = 8;
    const above = anchor.top - margin;
    const below = window.innerHeight - anchor.bottom - margin;
    const preferred = placement === 'bottom' ? 'bottom' : 'top';
    const fitsPreferred = (preferred === 'top' ? above : below) >= popup.height;
    const resolved = fitsPreferred ? preferred : above > below ? 'top' : 'bottom';
    const nextPosition = {
      top: Math.max(margin, Math.min(resolved === 'top' ? anchor.top - margin - popup.height : anchor.bottom + margin, window.innerHeight - popup.height - margin)),
      left: Math.max(margin, Math.min(anchor.left + anchor.width / 2 - popup.width / 2, window.innerWidth - popup.width - margin)),
      placement: resolved,
    };
    setPosition(previous => previous.top === nextPosition.top && previous.left === nextPosition.left && previous.placement === resolved ? previous : nextPosition);
  }, [content, isVisible, placement]);

  useEffect(() => {
    if (!isVisible) return undefined;
    window.addEventListener('scroll', hideTooltip, true);
    window.addEventListener('resize', hideTooltip);
    return () => {
      window.removeEventListener('scroll', hideTooltip, true);
      window.removeEventListener('resize', hideTooltip);
    };
  }, [hideTooltip, isVisible]);

  useEffect(() => {
    if (!isVisible) return undefined;
    const closeOnEscape = event => { if (event.key === 'Escape') hideTooltip(); };
    document.addEventListener('keydown', closeOnEscape);
    return () => document.removeEventListener('keydown', closeOnEscape);
  }, [hideTooltip, isVisible]);

  const tooltip = presence.present && hasContent
    ? createPortal(
        <div
          ref={tooltipRef}
          role="tooltip"
          aria-hidden={presence.closing}
          data-phase={presence.phase}
          className={`motion-tooltip pointer-events-none fixed z-[10000] max-w-[320px] overflow-hidden rounded-lg border border-slate-700/70 bg-slate-900 px-2.5 py-1.5 text-xs font-medium leading-relaxed text-white shadow-xl ring-1 ring-black/10 [overflow-wrap:anywhere] ${contentClassName}`}
          style={{
            top: `${position.top}px`,
            left: `${position.left}px`,
            width: 'max-content',
            maxWidth: 'min(320px, calc(100vw - 16px))',
            maxHeight: 'calc(100vh - 16px)',
          }}
        >
          {content}
        </div>,
        document.body
      )
    : null;

  // Keep the trigger's DOM, ref, classes and handlers intact. This matters for
  // table cells, positioned canvas nodes and buttons in tightly fitted groups.
  if (asChild) {
    return <><TooltipTrigger onShow={showTooltip} onHide={hideTooltip}>{children}</TooltipTrigger>{tooltip}</>;
  }

  return (
    <span
      ref={triggerRef}
      data-vane-tooltip-trigger=""
      className={`${hasDisplayClass ? '' : 'inline-flex'} ${className}`}
      onPointerEnter={showTooltip}
      onPointerLeave={hideTooltip}
      onFocus={showTooltip}
      onBlur={hideTooltip}
    >
      {children}
      {tooltip}
    </span>
  );
};

export default Tooltip;
