import { useCallback, useState } from 'react';

// Intersection alone can fire while the portal is still fading in. Keep the
// zero frame until its finite ancestor animations finish and it has been painted.
export function observeInsightPlayback(element, onStart) {
  let visible = false;
  let pending = false;
  let disposed = false;
  let revision = 0;
  let frame = 0;
  let observer;

  const cancel = () => {
    revision += 1;
    pending = false;
    cancelAnimationFrame(frame);
  };

  const start = () => {
    if (!visible || pending || disposed) return;
    pending = true;
    const version = revision;
    frame = requestAnimationFrame(async () => {
      const animations = [];
      for (let parent = element.parentElement; parent; parent = parent.parentElement) {
        animations.push(...(parent.getAnimations?.() || []));
      }
      await Promise.allSettled(animations
        .filter(animation => Number.isFinite(animation.effect?.getComputedTiming()?.endTime))
        .map(animation => animation.finished));
      if (disposed || !visible || version !== revision) return;
      frame = requestAnimationFrame(() => {
        frame = requestAnimationFrame(() => {
          if (disposed || !visible || version !== revision) return;
          disposed = true;
          observer?.disconnect();
          onStart();
        });
      });
    });
  };

  if (typeof IntersectionObserver === 'undefined') {
    visible = true;
    start();
  } else {
    observer = new IntersectionObserver(([entry]) => {
      visible = entry.isIntersecting && entry.intersectionRatio > 0;
      if (visible) start();
      else cancel();
    });
    observer.observe(element);
  }

  return () => {
    disposed = true;
    cancel();
    observer?.disconnect();
  };
}

export function useInsightPlayback(animate = true) {
  const [visible, setVisible] = useState(false);
  const enabled = Boolean(animate) && !(typeof window !== 'undefined'
    && window.matchMedia?.('(prefers-reduced-motion: reduce)').matches);
  const ref = useCallback(element => {
    if (!element || !enabled) return;
    const cleanup = observeInsightPlayback(element, () => setVisible(true));
    return () => {
      cleanup();
      setVisible(false);
    };
  }, [enabled]);
  return [ref, enabled && (visible || 'waiting')];
}
