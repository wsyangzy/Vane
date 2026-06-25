import { useCallback, useRef, useState } from 'react';

const DRAG_THRESHOLD_PX = 4;

const canScrollInDirection = (element, delta) => {
  if (!element || element.scrollWidth <= element.clientWidth) return false;
  if (delta < 0) return element.scrollLeft > 0;
  if (delta > 0) {
    const maxScrollLeft = element.scrollWidth - element.clientWidth;
    return element.scrollLeft < maxScrollLeft;
  }
  return false;
};

const getWheelDelta = (event) => (
  Math.abs(event.deltaX) > Math.abs(event.deltaY) ? event.deltaX : event.deltaY
);

const useHorizontalScroll = () => {
  const scrollRef = useRef(null);
  const dragStateRef = useRef(null);
  const suppressClickRef = useRef(false);
  const [isDragging, setIsDragging] = useState(false);

  const handleWheel = useCallback((event) => {
    const element = scrollRef.current;
    const delta = getWheelDelta(event);
    if (!event.shiftKey) return;
    if (!element || !canScrollInDirection(element, delta)) return;

    event.preventDefault();
    element.scrollLeft += delta;
  }, []);

  const handlePointerDown = useCallback((event) => {
    if (event.button !== 0 || !scrollRef.current) return;

    suppressClickRef.current = false;
    dragStateRef.current = {
      pointerId: event.pointerId,
      startX: event.clientX,
      scrollLeft: scrollRef.current.scrollLeft,
      moved: false,
      captured: false
    };
  }, []);

  const handlePointerMove = useCallback((event) => {
    const dragState = dragStateRef.current;
    const element = scrollRef.current;
    if (!dragState || !element) return;

    const deltaX = event.clientX - dragState.startX;
    if (Math.abs(deltaX) > DRAG_THRESHOLD_PX) {
      if (!dragState.moved) {
        dragState.moved = true;
        dragState.captured = true;
        event.currentTarget.setPointerCapture?.(dragState.pointerId);
        setIsDragging(true);
      }
      event.preventDefault();
    }
    element.scrollLeft = dragState.scrollLeft - deltaX;
  }, []);

  const stopDragging = useCallback((event) => {
    const dragState = dragStateRef.current;
    if (!dragState) return;

    if (dragState.moved) {
      suppressClickRef.current = true;
    }
    if (dragState.captured) {
      event.currentTarget.releasePointerCapture?.(dragState.pointerId);
    }
    dragStateRef.current = null;
    setIsDragging(false);
  }, []);

  const handleClickCapture = useCallback((event) => {
    if (!suppressClickRef.current) return;

    suppressClickRef.current = false;
    event.preventDefault();
    event.stopPropagation();
  }, []);

  const handleDragStart = useCallback((event) => {
    event.preventDefault();
  }, []);

  return {
    scrollRef,
    isDragging,
    scrollHandlers: {
      onWheel: handleWheel,
      onPointerDown: handlePointerDown,
      onPointerMove: handlePointerMove,
      onPointerUp: stopDragging,
      onPointerCancel: stopDragging,
      onClickCapture: handleClickCapture,
      onDragStart: handleDragStart
    }
  };
};

export default useHorizontalScroll;
