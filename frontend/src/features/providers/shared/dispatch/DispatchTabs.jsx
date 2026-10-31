import { useLayoutEffect, useRef } from 'react';
import './dispatchTabs.css';

function positionIndicator(root, indicator, animate) {
  const active = root.querySelector('[aria-selected="true"]');
  if (!active) return;
  const previousTransition = indicator.style.transition;
  if (!animate) indicator.style.transition = 'none';
  indicator.style.transform = `translateX(${active.offsetLeft}px)`;
  indicator.style.width = `${active.offsetWidth}px`;
  if (!animate) {
    void indicator.offsetWidth;
    indicator.style.transition = previousTransition;
  }
}

export default function DispatchTabs({ tabs, value, onChange, label, id }) {
  const rootRef = useRef(null);
  const indicatorRef = useRef(null);
  const positionedRef = useRef(false);

  useLayoutEffect(() => {
    positionIndicator(rootRef.current, indicatorRef.current, positionedRef.current);
    positionedRef.current = true;
  }, [value]);

  useLayoutEffect(() => {
    const root = rootRef.current;
    const indicator = indicatorRef.current;
    const observer = new ResizeObserver(() => positionIndicator(root, indicator, false));
    observer.observe(root);
    root.querySelectorAll('[role="tab"]').forEach((tab) => observer.observe(tab));
    return () => observer.disconnect();
  }, []);

  const onKeyDown = (event) => {
    const current = tabs.findIndex(([key]) => key === value);
    let next;
    if (event.key === 'ArrowRight') next = (current + 1) % tabs.length;
    else if (event.key === 'ArrowLeft') next = (current - 1 + tabs.length) % tabs.length;
    else if (event.key === 'Home') next = 0;
    else if (event.key === 'End') next = tabs.length - 1;
    else return;
    event.preventDefault();
    rootRef.current.querySelectorAll('[role="tab"]')[next].focus();
    onChange(tabs[next][0]);
  };

  return <div ref={rootRef} role="tablist" aria-label={label} className="t-tabs dispatch-tabs" onKeyDown={onKeyDown}>
    <span ref={indicatorRef} className="t-tabs-pill" aria-hidden="true" />
    {tabs.map(([key, title, Icon]) => <button key={key} id={`${id}-${key}-tab`} type="button" role="tab" aria-selected={value === key} aria-controls={`${id}-${key}-panel`} tabIndex={value === key ? 0 : -1} className="t-tab" onClick={() => onChange(key)}><Icon size={14} />{title}</button>)}
  </div>;
}
