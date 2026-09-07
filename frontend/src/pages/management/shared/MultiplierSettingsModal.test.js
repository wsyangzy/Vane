import test from 'node:test';
import assert from 'node:assert/strict';

const source = await import('node:fs').then(({ readFileSync }) => readFileSync(new URL('./MultiplierSettingsModal.jsx', import.meta.url), 'utf8'));

test('multiplier settings uses a draft modal with explicit save', () => {
  assert.match(source, /role="dialog"/);
  assert.match(source, /保存设置/);
  assert.match(source, /onSave\?\.\(draft\)/);
  assert.match(source, /parseMultiplierExpression/);
});

test('multiplier settings shows the selected model pricing as a filling reference', () => {
  assert.match(source, /getModelPricingsByProvider/);
  assert.match(source, /定价对照/);
  // 实际单价 = 基础单价 × 分项倍率 × 总倍率，与 tracker.go CalculateCost 的叠加口径一致
  assert.match(source, /basePrice \* resolveMultiplier\(key\) \* totalMultiplier/);
  // 无 priceKey 的字段即总倍率，不能参与自身的单价换算
  assert.match(source, /fields\.find\(\(field\) => !field\.priceKey\)/);
  // 定价拉取失败只是少了参考，不能阻断倍率配置
  assert.match(source, /\.catch\(\(\) => \{\}\)/);
});

test('multiplier settings can hide pricing reference for fixed-price editing', () => {
  assert.match(source, /showPricingReference = true/);
  assert.match(source, /if \(!showPricingReference\) return undefined/);
  assert.match(source, /placeholder=\{placeholder\}/);
});
