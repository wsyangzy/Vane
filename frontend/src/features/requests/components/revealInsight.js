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
