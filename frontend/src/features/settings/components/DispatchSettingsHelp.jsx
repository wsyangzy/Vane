import { useEffect, useId, useLayoutEffect, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import { ChevronRight } from 'lucide-react';

export default function DispatchSettingsHelp() {
  const [open, setOpen] = useState(false);
  const [position, setPosition] = useState({ top: 0, left: 0, maxHeight: undefined });
  const triggerRef = useRef(null);
  const popupRef = useRef(null);
  const popupId = useId();
  const titleId = useId();

  useLayoutEffect(() => {
    if (!open) return undefined;
    const updatePosition = () => {
      const trigger = triggerRef.current;
      const popup = popupRef.current;
      if (!trigger || !popup) return;
      const anchor = trigger.getBoundingClientRect();
      const margin = 8;
      const gap = 6;
      if (anchor.bottom <= margin || anchor.top >= window.innerHeight - margin) {
        setOpen(false);
        return;
      }
      const above = Math.max(0, anchor.top - gap - margin);
      const below = Math.max(0, window.innerHeight - anchor.bottom - gap - margin);
      const naturalHeight = popup.scrollHeight + popup.offsetHeight - popup.clientHeight;
      const placeAbove = below < naturalHeight && above > below;
      const maxHeight = placeAbove ? above : below;
      const height = Math.min(naturalHeight, maxHeight);
      setPosition({
        top: Math.max(margin, placeAbove ? anchor.top - gap - height : anchor.bottom + gap),
        left: Math.max(margin, Math.min(anchor.left, window.innerWidth - popup.offsetWidth - margin)),
        maxHeight,
      });
    };
    updatePosition();
    window.addEventListener('resize', updatePosition);
    return () => window.removeEventListener('resize', updatePosition);
  }, [open]);

  useEffect(() => {
    if (!open) return undefined;
    const outside = event => {
      if (!triggerRef.current?.contains(event.target) && !popupRef.current?.contains(event.target)) setOpen(false);
    };
    const escape = event => {
      if (event.key === 'Escape') {
        setOpen(false);
        triggerRef.current?.focus();
      }
    };
    const scroll = event => {
      // Keep scrolling inside the explanation usable when the viewport is short.
      if (!popupRef.current?.contains(event.target)) setOpen(false);
    };
    document.addEventListener('pointerdown', outside);
    document.addEventListener('keydown', escape);
    window.addEventListener('scroll', scroll, true);
    return () => {
      document.removeEventListener('pointerdown', outside);
      document.removeEventListener('keydown', escape);
      window.removeEventListener('scroll', scroll, true);
    };
  }, [open]);

  return <>
    <button
      ref={triggerRef}
      type="button"
      aria-expanded={open}
      aria-controls={open ? popupId : undefined}
      onClick={() => setOpen(value => !value)}
      className="my-2 flex w-full items-center gap-1 rounded-lg bg-indigo-50/60 px-2.5 py-2 text-left text-[11px] font-medium leading-5 text-indigo-700 hover:bg-indigo-50 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-indigo-300"
    >
      <ChevronRight size={13} className={`shrink-0 ${open ? 'rotate-90' : ''}`} aria-hidden="true" />
      配置与生效说明
    </button>
    {open && createPortal(
      <div
        ref={popupRef}
        id={popupId}
        role="region"
        aria-labelledby={titleId}
        className="fixed z-[10000] w-[min(520px,calc(100vw-16px))] overflow-y-auto overscroll-contain rounded-lg border border-indigo-100 bg-white p-3 text-[11px] leading-5 text-slate-600 shadow-xl [overflow-wrap:anywhere]"
        style={position}
      >
        <h4 id={titleId} className="font-semibold text-indigo-700">配置与生效说明</h4>
        <p className="mt-1 text-slate-500">以下设置由 Claude 与 Codex 共用。</p>
        <dl className="mt-3 space-y-2.5">
          <div>
            <dt className="font-medium text-slate-700">预算与等待</dt>
            <dd>方案与全局限制取较小值。达到上限后停止新尝试和恢复等待，已开始的正常响应继续。</dd>
          </div>
          <div>
            <dt className="font-medium text-slate-700">临时故障冷却</dt>
            <dd>新参数用于后续失败，已有冷却保留原到期时间；上游指定的等待时间优先。</dd>
          </div>
          <div>
            <dt className="font-medium text-slate-700">规则默认回退</dt>
            <dd>选择「跟随全局」时采用此处设置；单条规则的允许或禁止优先。</dd>
          </div>
        </dl>
      </div>,
      document.body,
    )}
  </>;
}
