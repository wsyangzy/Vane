import test from 'node:test';
import assert from 'node:assert/strict';

const source = await import('node:fs').then(({ readFileSync }) => readFileSync(new URL('./index.jsx', import.meta.url), 'utf8'));

test('credential-group billing separates standard and conditional long-context multipliers', () => {
  assert.match(source, /role="switch"/);
  assert.match(source, /普通成本倍率/);
  assert.match(source, /LongContextMultiplierSettings/);
  assert.match(source, /Long context 倍率已配置/);
  assert.match(source, /setLongContextSettingsOpen\(true\)/);
});

test('new credential groups disable long-context pricing and endpoint details show enabled multipliers', () => {
  assert.match(source, /disableLongContextPricing:\s*group\s*\?\s*Boolean\([\s\S]*?\)\s*:\s*true/);
  assert.match(source, /Long context 分组倍率/);
  assert.match(source, /longContextPricingEnabled\s*&&/);
});
