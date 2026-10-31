import { Children, useLayoutEffect, useRef } from 'react';
import { balanceHealthCards, restackHealthCards } from './healthCardLayout.js';

const MIN_CARD_WIDTH = 360;
const GAP = 12;

export default function HealthCardGrid({ children }) {
  const rootRef = useRef(null);
  const layoutRef = useRef(null);
  useLayoutEffect(() => {
    const root = rootRef.current;
    const cards = [...root.children];
    const keys = Children.toArray(children).map((child) => child.key);
    let frame = 0;
    let lastMeasurements = '';
    const setStyle = (element, property, value) => {
      if (element.style[property] !== value) element.style[property] = value;
    };
    const layout = () => {
      // Visual bounds include transforms and subpixel offsets. Measuring those
      // can reorder equal cards after every render, especially at fractional DPI.
      const width = root.clientWidth;
      if (!width) return;
      const count = Math.max(1, Math.min(cards.length, Math.floor((width + GAP) / (MIN_CARD_WIDTH + GAP))));
      const cardWidth = (width - GAP * (count - 1)) / count;
      // Set every width before measuring: wrapped text determines actual height.
      for (const card of cards) setStyle(card, 'width', `${cardWidth}px`);
      const heights = cards.map((card) => card.offsetHeight);
      const measurements = [width, ...heights].join(',');
      if (measurements === lastMeasurements) return;
      lastMeasurements = measurements;
      const identity = JSON.stringify([width, keys]);
      const next = layoutRef.current?.identity === identity
        ? restackHealthCards(heights, layoutRef.current.positions, GAP)
        : balanceHealthCards(heights, count, GAP);
      const { positions, height } = next;
      layoutRef.current = { identity, positions };
      cards.forEach((card, index) => {
        const { column, top } = positions[index];
        if (card.style.transform) card.style.removeProperty('transform');
        setStyle(card, 'left', `${column * (cardWidth + GAP)}px`);
        setStyle(card, 'top', `${top}px`);
      });
      setStyle(root, 'height', `${height}px`);
      if (root.dataset.measured !== 'true') root.dataset.measured = 'true';
    };
    layout();
    const observer = new ResizeObserver(() => {
      cancelAnimationFrame(frame);
      frame = requestAnimationFrame(layout);
    });
    observer.observe(root);
    cards.forEach((card) => observer.observe(card));
    return () => { observer.disconnect(); cancelAnimationFrame(frame); };
  }, [children]);

  return <div ref={rootRef} className="qh-health-grid">{Children.map(children, (child) => <div className="qh-health-item">{child}</div>)}</div>;
}
