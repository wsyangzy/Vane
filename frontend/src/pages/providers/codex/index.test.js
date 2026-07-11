import test from 'node:test';
import assert from 'node:assert/strict';

import { CODEX_REASONING_EFFORT_OPTIONS } from './reasoningEffort.js';
import * as reasoningEffortUtils from './reasoningEffort.js';

const source = await import('node:fs').then(({ readFileSync }) => readFileSync(new URL('./index.jsx', import.meta.url), 'utf8'));

test('credential-group long-context billing uses a switch and detailed multipliers', () => {
  assert.match(source, /role="switch"/);
  assert.match(source, /普通成本倍率/);
  assert.match(source, /LongContextMultiplierSettings/);
  assert.match(source, /Long context 倍率已配置/);
  assert.match(source, /setLongContextSettingsOpen\(true\)/);
  assert.match(source, /Long context 总倍率/);
  assert.match(source, /Long context 输入/);
  assert.match(source, /Long context 输出/);
  assert.match(source, /Long context 缓存创建/);
  assert.match(source, /Long context 缓存读取/);
});

test('new credential groups disable long-context pricing and endpoint details show enabled multipliers', () => {
  assert.match(source, /disableLongContextPricing:\s*group\s*\?\s*Boolean\([\s\S]*?\)\s*:\s*true/);
  assert.match(source, /Long context 分组倍率/);
  assert.match(source, /longContextPricingEnabled\s*&&/);
});

test('Codex global reasoning effort selector exposes all six levels', () => {
  const values = CODEX_REASONING_EFFORT_OPTIONS.map((option) => option.value);
  assert.deepEqual(values, ['low', 'medium', 'high', 'xhigh', 'max', 'ultra']);
});

test('Codex reasoning effort normalizer rejects obsolete or unknown levels', () => {
  const normalize = reasoningEffortUtils.normalizeCodexReasoningEffort;

  assert.equal(normalize?.('ultra'), 'ultra');
  assert.equal(normalize?.('minimal'), 'xhigh');
  assert.equal(normalize?.('unknown-level'), 'xhigh');
  assert.equal(normalize?.(''), 'xhigh');
});

test('Codex reasoning effort load state keeps an invalid source value dirty for write-back', () => {
  const getLoadState = reasoningEffortUtils.getCodexReasoningEffortLoadState;

  assert.deepEqual(getLoadState?.('minimal'), { value: 'xhigh', initialValue: 'minimal' });
  assert.deepEqual(getLoadState?.('ultra'), { value: 'ultra', initialValue: 'ultra' });
  assert.deepEqual(getLoadState?.(''), { value: 'xhigh', initialValue: 'xhigh' });
});
