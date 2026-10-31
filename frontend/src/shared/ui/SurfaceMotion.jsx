import { isValidElement, useCallback, useImperativeHandle, useLayoutEffect, useRef } from 'react';
import { motionDuration } from '@/shared/hooks/usePresence.js';

// Keep the portal's DOM and React context intact, including scoped canvas layers.
export default function SurfaceMotion({ content }) {
  const layerRef = useRef(null);
  const originalRef = content?.props?.ref;
  const setLayer = useCallback(node => { layerRef.current = node; }, []);
  useImperativeHandle(originalRef, () => layerRef.current, []);

  useLayoutEffect(() => {
    const layer = layerRef.current;
    const duration = motionDuration('--modal-open-dur', 250);
    if (!layer || !duration) return undefined;
    const panel = layer.matches('[role="dialog"], [aria-modal="true"]') ? layer
      : layer.querySelector('[role="dialog"], [aria-modal="true"], .relative.bg-white') || layer;
    const easing = getComputedStyle(document.documentElement).getPropertyValue('--ease-smooth-out').trim() || 'ease-out';
    const animations = [layer.animate([{ opacity: 0 }, { opacity: 1 }], { duration, easing })];
    if (panel !== layer) {
      animations.push(panel.animate([
        { transform: 'translateY(6px) scale(.98)' },
        { transform: 'translateY(0) scale(1)' }
      ], { duration, easing }));
    }
    return () => animations.forEach(animation => animation.cancel());
  }, []);

  if (!isValidElement(content) || typeof content.type !== 'string') return content;
  const Element = content.type;
  return <Element key={content.key} {...content.props} ref={setLayer} data-motion-portal="" />;
}
