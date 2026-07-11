import test from 'node:test';
import assert from 'node:assert/strict';

const source = await import('node:fs').then(({ readFileSync }) => readFileSync(new URL('./LongContextMultiplierSettings.jsx', import.meta.url), 'utf8'));

test('long-context multiplier settings uses a draft modal with explicit save', () => {
  assert.match(source, /role="dialog"/);
  assert.match(source, /保存设置/);
  assert.match(source, /onSave\?\.\(draft\)/);
  assert.match(source, /parseMultiplierExpression/);
});
