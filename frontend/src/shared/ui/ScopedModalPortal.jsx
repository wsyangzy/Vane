import { useLayoutEffect, useRef } from 'react';
import { createPortal } from 'react-dom';

const focusable = 'button:not(:disabled), input:not(:disabled), textarea:not(:disabled), select:not(:disabled), [href], [tabindex="0"]';

export default function ScopedModalPortal({ content, scope }) {
  const layerRef = useRef(null);
  const { target, registerLayer, isTopLayer } = scope;
  useLayoutEffect(() => {
    const layer = layerRef.current;
    const previousFocus = document.activeElement;
    const unregister = registerLayer(layer);
    const autofocus = layer.querySelector('[autofocus]') || layer.querySelector('input:not(:disabled)') || layer.querySelector(focusable);
    autofocus?.focus();
    return () => {
      unregister();
      if (previousFocus?.isConnected && !previousFocus.closest('[inert]')) previousFocus.focus();
    };
  }, [target, registerLayer]);

  const onKeyDown = (event) => {
    if (!isTopLayer(layerRef.current) || event.defaultPrevented) return;
    if (event.key === 'Escape') {
      // CustomSelect owns its body portal and handles Escape on window.
      if (document.querySelector('[role="listbox"]')) return;
      event.stopPropagation();
      event.preventDefault();
      const buttons = [...layerRef.current.querySelectorAll('button:not(:disabled)')];
      const close = buttons.find((button) => button.getAttribute('aria-label') === '关闭') || buttons.find((button) => /^(取消|关闭)$/.test(button.textContent.trim()));
      close?.click();
    }
    if (event.key === 'Tab' && layerRef.current.contains(event.target)) {
      const controls = [...layerRef.current.querySelectorAll(focusable)].filter((element) => element.getClientRects().length);
      if (event.shiftKey && document.activeElement === controls[0]) {
        event.preventDefault();
        controls.at(-1)?.focus();
      } else if (!event.shiftKey && document.activeElement === controls.at(-1)) {
        event.preventDefault();
        controls[0]?.focus();
      }
    }
  };
  return createPortal(<div ref={layerRef} className="canvas-floating-layer" data-canvas-operation onKeyDown={onKeyDown}>{content}</div>, target);
}
