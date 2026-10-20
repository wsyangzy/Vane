import { useCallback, useEffect, useRef, useState } from 'react';
import { createPortal } from 'react-dom';

const Tooltip = ({
  content,
  children,
  placement = 'top',
  disabled = false,
  className = '',
  contentClassName = ''
}) => {
  const [isVisible, setIsVisible] = useState(false);
  const [position, setPosition] = useState({ top: 0, left: 0, placement });
  const triggerRef = useRef(null);

  const hasContent = content !== undefined && content !== null && content !== '';
  const hasDisplayClass = /\b(?:block|inline-block|inline-flex|flex|grid|inline-grid)\b/.test(className);

  const showTooltip = useCallback(() => {
    if (disabled || !hasContent || !triggerRef.current) return;

    const rect = triggerRef.current.getBoundingClientRect();
    const preferredPlacement = placement === 'bottom' ? 'bottom' : 'top';
    const resolvedPlacement =
      preferredPlacement === 'top' && rect.top < 56
        ? 'bottom'
        : preferredPlacement === 'bottom' && window.innerHeight - rect.bottom < 56
          ? 'top'
          : preferredPlacement;

    setPosition({
      top: resolvedPlacement === 'top' ? rect.top - 8 : rect.bottom + 8,
      left: Math.min(Math.max(rect.left + rect.width / 2, 16), window.innerWidth - 16),
      placement: resolvedPlacement
    });
    setIsVisible(true);
  }, [disabled, hasContent, placement]);

  const hideTooltip = useCallback(() => {
    setIsVisible(false);
  }, []);

  useEffect(() => {
    if (!isVisible) return undefined;
    window.addEventListener('scroll', hideTooltip, true);
    window.addEventListener('resize', hideTooltip);
    return () => {
      window.removeEventListener('scroll', hideTooltip, true);
      window.removeEventListener('resize', hideTooltip);
    };
  }, [hideTooltip, isVisible]);

  const tooltip = isVisible && hasContent
    ? createPortal(
        <div
          role="tooltip"
          className={`pointer-events-none fixed z-[10000] max-w-[320px] rounded-lg border border-slate-700/70 bg-slate-900 px-2.5 py-1.5 text-xs font-medium leading-relaxed text-white shadow-xl ring-1 ring-black/10 ${
            position.placement === 'top'
              ? '-translate-x-1/2 -translate-y-full'
              : '-translate-x-1/2'
          } ${contentClassName}`}
          style={{
            top: `${position.top}px`,
            left: `${position.left}px`
          }}
        >
          {content}
        </div>,
        document.body
      )
    : null;

  return (
    <span
      ref={triggerRef}
      className={`${hasDisplayClass ? '' : 'inline-flex'} ${className}`}
      onMouseEnter={showTooltip}
      onMouseLeave={hideTooltip}
      onFocus={showTooltip}
      onBlur={hideTooltip}
    >
      {children}
      {tooltip}
    </span>
  );
};

export default Tooltip;
