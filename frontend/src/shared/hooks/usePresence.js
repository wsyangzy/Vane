import { useLayoutEffect, useRef, useState } from 'react';

export const motionDuration = (property, fallback) => {
  if (window.matchMedia('(prefers-reduced-motion: reduce)').matches) return 0;
  const value = getComputedStyle(document.documentElement).getPropertyValue(property).trim();
  const duration = parseFloat(value);
  return Number.isFinite(duration) ? duration * (value.endsWith('ms') ? 1 : 1000) : fallback;
};

// Retain a closing surface, while making it inert, until its exit finishes.
export function usePresence(open, kind = 'dropdown') {
  const [state, setState] = useState({ open, phase: open ? 'entering' : 'closed' });
  const wasPresent = useRef(open);
  if (state.open !== open) {
    setState({ open, phase: open ? 'entering' : state.phase === 'closed' ? 'closed' : 'closing' });
  }
  const phase = state.phase;

  useLayoutEffect(() => {
    let frame;
    let nextFrame;
    let timer;
    if (open) {
      wasPresent.current = true;
      frame = requestAnimationFrame(() => {
        nextFrame = requestAnimationFrame(() => setState({ open: true, phase: 'open' }));
      });
    } else {
      if (!wasPresent.current) return undefined;
      const duration = motionDuration(`--${kind}-close-dur`, 150);
      timer = setTimeout(() => { wasPresent.current = false; setState({ open: false, phase: 'closed' }); }, duration);
    }
    return () => {
      cancelAnimationFrame(frame);
      cancelAnimationFrame(nextFrame);
      clearTimeout(timer);
    };
  }, [open, kind]);

  return { present: open || phase !== 'closed', phase, closing: !open, className: phase === 'open' ? 'is-open' : phase === 'closing' ? 'is-closing' : '' };
}
