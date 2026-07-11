import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';

const source = fs.readFileSync(new URL('./ModelTag.jsx', import.meta.url), 'utf8');

test('model rewrite indicator stays inside the truncated model tag boundary', () => {
  assert.match(source, /pr-5/);
  assert.match(source, /right-0/);
  assert.doesNotMatch(source, /left-full/);
});
