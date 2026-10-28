import test from 'node:test';
import assert from 'node:assert/strict';
import { balanceHealthCards, restackHealthCards } from './healthCardLayout.js';

test('growing and shrinking a card keep other columns fixed and update only the following cards in its column', () => {
  const heights = [200, 150, 180, 90, 140, 110];
  const before = balanceHealthCards(heights, 3);
  const growing = 0;
  const column = before.positions[growing].column;
  const changed = heights.map((height, index) => height + (index === growing ? 275 : 0));
  const after = restackHealthCards(changed, before.positions);
  after.positions.forEach((position, index) => {
    assert.equal(position.column, before.positions[index].column);
    assert.equal(position.top, before.positions[index].top + (position.column === column && index > growing ? 275 : 0));
  });
  assert.equal(after.height, Math.max(...bottoms(changed, after.positions, 3)));
  assert.deepEqual(restackHealthCards(heights, after.positions), before);
  assert.deepEqual(restackHealthCards([], []), { positions: [], height: 0 });
});

const bottoms = (heights, positions, columns) => Array.from({ length: columns }, (_, column) => Math.max(0, ...positions.flatMap((position, index) => position.column === column ? [position.top + heights[index]] : [])));

test('unequal card heights balance by total height instead of card count', () => {
  const heights = [700, 700, 100, 100, 100, 700];
  const { positions, height } = balanceHealthCards(heights, 3);
  assert.deepEqual(bottoms(heights, positions, 3), [812, 812, 812]);
  assert.equal(height, 812);
  assert.equal(new Set(positions.map((position) => position.column)).size, 3);
});

test('cards retain source order in each column with exact gaps and no overlap', () => {
  const heights = [123.5, 280, 50.25, 405, 110, 70, 91.75];
  const { positions, height } = balanceHealthCards(heights, 3);
  for (let column = 0; column < 3; column++) {
    const indices = positions.flatMap((position, index) => position.column === column ? [index] : []);
    assert.equal(positions[indices[0]].top, 0);
    for (let index = 1; index < indices.length; index++) {
      const previous = indices[index - 1];
      assert.equal(positions[indices[index]].top, positions[previous].top + heights[previous] + 12);
    }
  }
  assert.equal(height, Math.max(...bottoms(heights, positions, 3)));
  assert.deepEqual(balanceHealthCards(heights, 3).positions, positions);
});

test('a narrow container preserves the original card order and removes trailing gaps', () => {
  assert.deepEqual(balanceHealthCards([200, 80, 125], 1), {
    positions: [{ column: 0, top: 0 }, { column: 0, top: 212 }, { column: 0, top: 304 }], height: 429,
  });
  assert.deepEqual(balanceHealthCards([], 3), { positions: [], height: 0 });
  assert.deepEqual(balanceHealthCards([100], 3), { positions: [{ column: 0, top: 0 }], height: 100 });
});

test('changed card heights recompute a balanced layout without retaining old positions', () => {
  const heights = [500, 100, 100, 100, 100, 100];
  const before = balanceHealthCards([100, 100, 100, 100, 100, 100], 2);
  const after = balanceHealthCards(heights, 2);
  assert.notDeepEqual(after.positions, before.positions);
  assert.deepEqual(bottoms(heights, after.positions, 2), [500, 548]);
  assert.equal(after.height, 548);
});

test('mixed channel sizes improve a greedy partition that leaves a whole card of bottom whitespace', () => {
  const heights = [640, 555, 490, 455, 380, 310, 260, 205, 205, 205];
  const { positions } = balanceHealthCards(heights, 3);
  const values = bottoms(heights, positions, 3);
  assert.ok(Math.max(...values) - Math.min(...values) <= 48);
  assert.ok(Math.max(...values) < 1391);
});

test('alternative combinations improve a layout that individual moves and swaps cannot balance further', () => {
  const heights = [835, 523, 212, 373, 287, 212, 373, 448, 212, 212];
  const { positions } = balanceHealthCards(heights, 3);
  const values = bottoms(heights, positions, 3);
  assert.equal(Math.max(...values) - Math.min(...values), 61);
});

test('indivisible cards keep their natural height even when exact alignment is impossible', () => {
  const heights = [700, 200, 200];
  const { positions, height } = balanceHealthCards(heights, 3);
  assert.deepEqual(bottoms(heights, positions, 3), heights);
  assert.equal(height, 700);
});
