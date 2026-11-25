import { isRequestInProgress } from './requestOutcome.js';

const milliseconds = value => {
  if (value == null || value === '') return null;
  const number = Number(value);
  return Number.isFinite(number) && number >= 0 ? number : null;
};

const requestStartedAt = request => {
  const value = request.start_time ?? request.startTime ?? request.timestamp;
  if (typeof value === 'number') return milliseconds(value);
  if (value instanceof Date) return milliseconds(value.getTime());
  if (typeof value !== 'string' || !value.trim()) return null;
  // Wails sends local display time; HTTP may send RFC3339 with an offset.
  const timestamp = Date.parse(value.trim().replace(/^(\d{4}-\d{2}-\d{2})\s+/, '$1T'));
  return milliseconds(timestamp);
};

export function getRequestLatency(request = {}, now = 0, observedAt = now) {
  const active = isRequestInProgress(request);
  const duration = milliseconds(request.duration ?? request.duration_ms ?? request.response_time) ?? 0;
  const firstByte = milliseconds(request.firstByteMs ?? request.first_byte_ms);
  const startedAt = requestStartedAt(request) ?? observedAt;
  const elapsed = active ? Math.max(duration, Math.floor(now - startedAt), 0) : duration;
  const waitingForFirstByte = active && firstByte == null;

  return {
    durationMs: elapsed,
    firstByteMs: waitingForFirstByte ? elapsed : firstByte,
    durationLive: active,
    firstByteLive: waitingForFirstByte,
  };
}

// All visible active latency cells share one clock. Polling replaces request
// snapshots without restarting it; hidden pages and the last unsubscribe stop it.
export function createRequestClock(document = globalThis.document) {
  const listeners = new Set();
  let snapshot = Date.now();
  let timer;

  const update = () => {
    snapshot = Date.now();
    for (const listener of listeners) listener();
  };
  const stop = () => {
    clearInterval(timer);
    timer = undefined;
  };
  const onVisibilityChange = () => {
    stop();
    if (!document?.hidden) {
      update();
      timer = setInterval(update, 100);
    }
  };

  return {
    getSnapshot: () => snapshot,
    subscribe(listener) {
      listeners.add(listener);
      if (listeners.size === 1) {
        document?.addEventListener('visibilitychange', onVisibilityChange);
        onVisibilityChange();
      }
      return () => {
        listeners.delete(listener);
        if (listeners.size === 0) {
          stop();
          document?.removeEventListener('visibilitychange', onVisibilityChange);
        }
      };
    },
  };
}
