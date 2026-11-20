import { useCallback, useState } from 'react';

export function revealInsight(chart) {
  if (!chart || chart.hasAttribute('data-enter')) return;
  if (typeof IntersectionObserver === 'undefined') {
    chart.setAttribute('data-enter', 'true');
    return;
  }

  const observer = new IntersectionObserver(([entry]) => {
    if (entry.intersectionRatio < 0.1) return;
    chart.setAttribute('data-enter', 'true');
    observer.disconnect();
  }, { threshold: 0.1 });
  observer.observe(chart);
  return () => observer.disconnect();
}

export function useInsightPlayback(animate) {
  const [visible, setVisible] = useState(false);
  const ref = useCallback(element => {
    if (!element || !animate) return;
    if (typeof IntersectionObserver === 'undefined') {
      setVisible(true);
      return;
    }
    const observer = new IntersectionObserver(([entry]) => {
      if (!entry.isIntersecting) return;
      setVisible(true);
      observer.disconnect();
    });
    observer.observe(element);
    return () => observer.disconnect();
  }, [animate]);
  return [ref, animate && (visible || 'waiting')];
}
