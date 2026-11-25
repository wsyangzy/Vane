import test from 'node:test';
import assert from 'node:assert/strict';
import { createRequestClock, getRequestLatency } from './requestLatency.js';

const start = Date.parse('2026-11-25T07:30:00.000Z');
const request = { timestamp: '2026-11-25T07:30:00.000Z', status: 'forwarding', duration: 0, firstByteMs: null };

test('both live latency values start at zero and advance while waiting for the first byte', () => {
  for (const elapsed of [0, 100, 950, 1_500, 25_000]) {
    assert.deepEqual(getRequestLatency(request, start + elapsed), {
      durationMs: elapsed, firstByteMs: elapsed, durationLive: true, firstByteLive: true,
    });
  }
});

test('receiving the first byte freezes only its measured time, including an actual zero', () => {
  for (const firstByteMs of [0, 125, 1_250]) {
    for (const elapsed of [1_500, 3_000]) {
      assert.deepEqual(getRequestLatency({ ...request, firstByteMs }, start + elapsed), {
        durationMs: elapsed, firstByteMs, durationLive: true, firstByteLive: false,
      });
    }
  }
});

test('terminal states use the recorded duration and do not invent a first-byte result', () => {
  for (const status of ['completed', 'failed', 'cancelled', 'timeout', 'incomplete', 'partial_failed']) {
    assert.deepEqual(getRequestLatency({ ...request, status, duration: 1_234 }, start + 90_000), {
      durationMs: 1_234, firstByteMs: null, durationLive: false, firstByteLive: false,
    });
    assert.equal(getRequestLatency({ ...request, status, duration: 1_234, firstByteMs: 321 }, start + 90_000).firstByteMs, 321);
  }
  assert.equal(getRequestLatency({ ...request, in_progress: false, duration: 99 }, start + 5_000).durationMs, 99);
});

test('refreshing and retrying a request retains its original start time', () => {
  for (const status of ['pending', 'received', 'forwarding', 'processing', 'streaming', 'retry', 'suspended', 'awaiting_endpoint']) {
    assert.equal(getRequestLatency({ ...request, status }, start + 12_000, start + 11_000).durationMs, 12_000);
  }
  assert.deepEqual(getRequestLatency({ ...request, duration: 8_000 }, start + 5_000), {
    durationMs: 8_000, firstByteMs: 8_000, durationLive: true, firstByteLive: true,
  });
});

test('RFC3339 offsets and Wails local timestamps produce the expected elapsed time', () => {
  assert.equal(getRequestLatency({ ...request, start_time: '2026-11-25T15:30:00+08:00' }, start + 500).durationMs, 500);
  const localStart = new Date(2026, 10, 25, 15, 30, 0).getTime();
  assert.equal(getRequestLatency({ ...request, timestamp: '2026-11-25 15:30:00' }, localStart + 900).durationMs, 900);
  assert.equal(getRequestLatency({ ...request, startTime: new Date(start) }, start + 500).durationMs, 500);
});

test('invalid metadata falls back to observation time without negative or invalid counters', () => {
  for (const timestamp of ['', 'unavailable', null]) {
    const invalid = { ...request, timestamp, duration: 'invalid', firstByteMs: -1 };
    assert.equal(getRequestLatency(invalid, start, start).durationMs, 0);
    assert.equal(getRequestLatency(invalid, start + 500, start).firstByteMs, 500);
  }
  assert.equal(getRequestLatency(request, start - 500).durationMs, 0);
  assert.deepEqual(getRequestLatency({ status: 'forwarding', start_time: request.timestamp, duration_ms: '123', first_byte_ms: '45' }, start + 500), {
    durationMs: 500, firstByteMs: 45, durationLive: true, firstByteLive: false,
  });
});

test('active cells share one clock and unsubscribe independently', context => {
  context.mock.timers.enable({ apis: ['Date', 'setInterval'], now: start });
  const schedule = context.mock.method(globalThis, 'setInterval');
  const clock = createRequestClock();
  let firstUpdates = 0;
  let secondUpdates = 0;
  const stopFirst = clock.subscribe(() => { firstUpdates++; });
  const stopSecond = clock.subscribe(() => { secondUpdates++; });
  assert.equal(schedule.mock.callCount(), 1);
  assert.equal(clock.getSnapshot(), start);
  assert.equal(clock.getSnapshot(), clock.getSnapshot());
  context.mock.timers.tick(100);
  assert.equal(clock.getSnapshot(), start + 100);
  assert.equal(firstUpdates, 2);
  assert.equal(secondUpdates, 1);
  stopFirst();
  context.mock.timers.tick(100);
  assert.equal(firstUpdates, 2);
  assert.equal(secondUpdates, 2);
  stopSecond();
  context.mock.timers.tick(1_000);
  assert.equal(secondUpdates, 2);
  assert.equal(clock.getSnapshot(), start + 200);
});

test('hidden pages pause the clock and resume at current time without losing elapsed time', context => {
  context.mock.timers.enable({ apis: ['Date', 'setInterval'], now: start });
  const document = new EventTarget();
  document.hidden = false;
  const clock = createRequestClock(document);
  let updates = 0;
  const unsubscribe = clock.subscribe(() => { updates++; });
  context.mock.timers.tick(100);
  document.hidden = true;
  document.dispatchEvent(new Event('visibilitychange'));
  context.mock.timers.tick(5_000);
  assert.equal(updates, 2);
  assert.equal(clock.getSnapshot(), start + 100);
  document.hidden = false;
  document.dispatchEvent(new Event('visibilitychange'));
  assert.equal(clock.getSnapshot(), start + 5_100);
  assert.equal(getRequestLatency(request, clock.getSnapshot()).durationMs, 5_100);
  context.mock.timers.tick(100);
  assert.equal(updates, 4);
  unsubscribe();
  document.dispatchEvent(new Event('visibilitychange'));
  context.mock.timers.tick(100);
  assert.equal(updates, 4);
});

test('a request clock mounted on a hidden page remains idle until it is visible', context => {
  context.mock.timers.enable({ apis: ['Date', 'setInterval'], now: start });
  const schedule = context.mock.method(globalThis, 'setInterval');
  const document = new EventTarget();
  document.hidden = true;
  const clock = createRequestClock(document);
  const unsubscribe = clock.subscribe(() => {});
  assert.equal(schedule.mock.callCount(), 0);
  context.mock.timers.tick(1_000);
  document.hidden = false;
  document.dispatchEvent(new Event('visibilitychange'));
  assert.equal(schedule.mock.callCount(), 1);
  assert.equal(clock.getSnapshot(), start + 1_000);
  unsubscribe();
});
