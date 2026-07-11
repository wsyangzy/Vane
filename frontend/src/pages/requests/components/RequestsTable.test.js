import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';

const source = fs.readFileSync(new URL('./RequestsTable.jsx', import.meta.url), 'utf8');

test('request table never exposes a horizontal scrollbar', () => {
  assert.match(source, /overflow-x-hidden/);
  assert.doesNotMatch(source, /overflow-x-auto/);
});
