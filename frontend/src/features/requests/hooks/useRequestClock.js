import { useSyncExternalStore } from 'react';
import { createRequestClock } from '../utils/requestLatency.js';

const clock = createRequestClock();
const idleSnapshot = () => 0;
const idleSubscribe = () => () => {};

export function useRequestClock(active) {
  return useSyncExternalStore(
    active ? clock.subscribe : idleSubscribe,
    active ? clock.getSnapshot : idleSnapshot,
    idleSnapshot,
  );
}
