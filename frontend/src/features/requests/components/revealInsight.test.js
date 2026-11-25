import test from 'node:test';
import assert from 'node:assert/strict';
import { observeInsightPlayback } from './revealInsight.js';

function playbackEnvironment(t, useObserver = true) {
  const names = ['requestAnimationFrame', 'cancelAnimationFrame', 'IntersectionObserver'];
  const previous = names.map(name => Object.getOwnPropertyDescriptor(globalThis, name));
  const frames = new Map();
  const observers = [];
  const cleanups = [];
  let nextFrame = 0;
  globalThis.requestAnimationFrame = callback => {
    frames.set(++nextFrame, callback);
    return nextFrame;
  };
  globalThis.cancelAnimationFrame = id => frames.delete(id);
  globalThis.IntersectionObserver = useObserver ? class {
    constructor(callback) { this.callback = callback; observers.push(this); }
    observe() {}
    disconnect() { this.disconnected = true; }
    show(visible, ratio = visible ? 1 : 0) {
      this.callback([{ isIntersecting: visible, intersectionRatio: ratio }]);
    }
  } : undefined;
  t.after(() => {
    cleanups.forEach(cleanup => cleanup());
    names.forEach((name, index) => {
      if (previous[index]) Object.defineProperty(globalThis, name, previous[index]);
      else delete globalThis[name];
    });
  });
  return {
    element: { parentElement: null }, observers, frames,
    cleanup: callback => cleanups.push(callback),
    async frame() {
      const callbacks = [...frames.values()];
      frames.clear();
      callbacks.forEach(callback => callback(0));
      await Promise.resolve();
      await Promise.resolve();
      await Promise.resolve();
    },
  };
}

test('offscreen insights stay at their zero frame and start once after visible paints', async t => {
  const env = playbackEnvironment(t);
  let starts = 0;
  const cleanup = observeInsightPlayback(env.element, () => { starts += 1; });
  env.cleanup(cleanup);
  env.observers[0].show(false);
  await env.frame();
  assert.equal(starts, 0);
  env.observers[0].show(true, 0);
  assert.equal(env.frames.size, 0);
  env.observers[0].show(true);
  await env.frame();
  await env.frame();
  assert.equal(starts, 0);
  await env.frame();
  assert.equal(starts, 1);
  assert.equal(env.observers[0].disconnected, true);
  env.observers[0].show(true);
  await env.frame();
  assert.equal(starts, 1);
});

test('portal entrance finishes before playback and infinite decorations do not block it', async t => {
  const env = playbackEnvironment(t);
  let finishEntrance;
  const entrance = new Promise(resolve => { finishEntrance = resolve; });
  env.element.parentElement = {
    parentElement: null,
    getAnimations: () => [
      { effect: { getComputedTiming: () => ({ endTime: 250 }) }, finished: entrance },
      { effect: { getComputedTiming: () => ({ endTime: Infinity }) }, finished: new Promise(() => {}) },
    ],
  };
  let starts = 0;
  env.cleanup(observeInsightPlayback(env.element, () => { starts += 1; }));
  env.observers[0].show(true);
  await env.frame();
  await env.frame();
  assert.equal(starts, 0);
  finishEntrance();
  await env.frame();
  await env.frame();
  assert.equal(starts, 0);
  await env.frame();
  assert.equal(starts, 1);
});

test('scrolling away cancels a queued reveal and reentry starts from a fresh visible frame', async t => {
  const env = playbackEnvironment(t);
  let finishEntrance;
  const entrance = new Promise(resolve => { finishEntrance = resolve; });
  env.element.parentElement = {
    parentElement: null,
    getAnimations: () => [{ effect: { getComputedTiming: () => ({ endTime: 250 }) }, finished: entrance }],
  };
  let starts = 0;
  env.cleanup(observeInsightPlayback(env.element, () => { starts += 1; }));
  env.observers[0].show(true);
  await env.frame();
  env.observers[0].show(false);
  finishEntrance();
  await env.frame();
  await env.frame();
  assert.equal(starts, 0);
  assert.equal(env.frames.size, 0);
  env.observers[0].show(true);
  await env.frame();
  await env.frame();
  assert.equal(starts, 0);
  await env.frame();
  assert.equal(starts, 1);
});

test('closing while waiting cancels playback and reopening gets a new entrance', async t => {
  const env = playbackEnvironment(t);
  let starts = 0;
  const firstCleanup = observeInsightPlayback(env.element, () => { starts += 1; });
  env.observers[0].show(true);
  await env.frame();
  firstCleanup();
  await env.frame();
  assert.equal(starts, 0);
  assert.equal(env.frames.size, 0);
  assert.equal(env.observers[0].disconnected, true);
  env.cleanup(observeInsightPlayback(env.element, () => { starts += 1; }));
  env.observers[1].show(true);
  await env.frame();
  await env.frame();
  assert.equal(starts, 0);
  await env.frame();
  assert.equal(starts, 1);
});

test('without IntersectionObserver playback still allows the zero frame to paint', async t => {
  const env = playbackEnvironment(t, false);
  let starts = 0;
  env.cleanup(observeInsightPlayback(env.element, () => { starts += 1; }));
  await env.frame();
  await env.frame();
  assert.equal(starts, 0);
  await env.frame();
  assert.equal(starts, 1);
});
