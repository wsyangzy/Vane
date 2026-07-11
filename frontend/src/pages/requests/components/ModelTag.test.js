import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';

const source = fs.readFileSync(new URL('./ModelTag.jsx', import.meta.url), 'utf8');

test('model tag keeps the full name and rewrite indicator on one line', () => {
  assert.match(source, /whitespace-nowrap/);
  assert.match(source, /rewriteIndicator\s*&&/);
  assert.match(source, /shrink-0/);
  assert.doesNotMatch(source, /truncate/);
  assert.doesNotMatch(source, /left-full/);
});
