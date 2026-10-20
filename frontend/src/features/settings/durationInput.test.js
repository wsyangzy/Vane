import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';

const durationInputSource = readFileSync(
  new URL('./components/DurationInput.jsx', import.meta.url),
  'utf8'
);
const customSelectSource = readFileSync(
  new URL('../../shared/ui/CustomSelect.jsx', import.meta.url),
  'utf8'
);

test('duration input renders number and unit as one compound control', () => {
  assert.match(durationInputSource, /focus-within:border-indigo-500/);
  assert.match(durationInputSource, /<span className="h-5 w-px/);
  assert.match(durationInputSource, /embedded/);
  assert.match(durationInputSource, /ariaLabel="时间单位"/);
});

test('custom select portals its animated menu out of transformed cards', () => {
  assert.match(customSelectSource, /createPortal\(/);
  assert.match(customSelectSource, /document\.body/);
  assert.match(customSelectSource, /t-dropdown \$\{dropdownState\} fixed/);
  assert.match(customSelectSource, /dropdownRef\.current\?\.contains\(event\.target\)/);
});
