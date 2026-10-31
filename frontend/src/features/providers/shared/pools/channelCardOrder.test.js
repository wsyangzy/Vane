import assert from 'node:assert/strict';
import test from 'node:test';
import { moveChannelCard, reconcileChannelCardOrder } from './channelCardOrder.js';

test('saved display order survives refreshed backend ordering', () => {
  assert.deepEqual(reconcileChannelCardOrder(['a', 'b', 'c'], ['c', 'a', 'b']), ['c', 'a', 'b']);
});

test('deleted cards disappear and new cards append without losing the saved order', () => {
  assert.deepEqual(reconcileChannelCardOrder(['a', 'c', 'd'], ['c', 'removed', 'a']), ['c', 'a', 'd']);
});

test('duplicate or unavailable saved entries do not duplicate cards', () => {
  assert.deepEqual(reconcileChannelCardOrder(['a', 'b', 'b'], ['b', 'b', 'missing']), ['b', 'a']);
});

test('invalid stored order falls back to the supplied card order', () => {
  for (const saved of [null, undefined, 'a', {}, 1]) {
    assert.deepEqual(reconcileChannelCardOrder(['a', 'b'], saved), ['a', 'b']);
  }
});

test('dragging a card forward or backward inserts it at the target position', () => {
  assert.deepEqual(moveChannelCard(['a', 'b', 'c', 'd'], 'a', 'c'), ['b', 'c', 'a', 'd']);
  assert.deepEqual(moveChannelCard(['a', 'b', 'c', 'd'], 'd', 'b'), ['a', 'd', 'b', 'c']);
});

test('cancelled, missing, or unchanged targets keep the existing order', () => {
  const keys = ['a', 'b', 'c'];
  for (const [source, target] of [['a', null], ['missing', 'b'], ['a', 'missing'], ['b', 'b']]) {
    assert.equal(moveChannelCard(keys, source, target), keys);
  }
});

test('display sorting never mutates the incoming or stored order', () => {
  const keys = Object.freeze(['a', 'b', 'c']);
  const saved = Object.freeze(['c', 'b']);
  assert.deepEqual(reconcileChannelCardOrder(keys, saved), ['c', 'b', 'a']);
  assert.deepEqual(moveChannelCard(keys, 'a', 'c'), ['b', 'c', 'a']);
  assert.deepEqual(keys, ['a', 'b', 'c']);
  assert.deepEqual(saved, ['c', 'b']);
});
