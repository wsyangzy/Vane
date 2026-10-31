import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

import {
  buildInitialBillingRuleRows,
  createDefaultBillingRuleRow,
  createBillingRuleRow,
  getBillingRuleEffectiveMultipliers,
  parseBillingRuleRows
} from './billingRuleRows.js';

const editorSource = readFileSync(new URL('../components/BillingRuleEditor.jsx', import.meta.url), 'utf8');

test('组合规则用 Long context 开关替代上下文下拉选择', () => {
  assert.match(editorSource, /role="switch"/);
  assert.match(editorSource, /createBillingRuleRow\(\{ context: 'normal' \}\)/);
  assert.match(editorSource, /MultiplierSettingsModal/);
  assert.doesNotMatch(editorSource, /CONTEXT_OPTIONS/);
  assert.doesNotMatch(editorSource, /const RuleFields/);
});

test('组合规则卡片保存为普通规则和可选的 Long context 规则', () => {
  const standard = createDefaultBillingRuleRow({ costMultiplier: '4/3' });
  standard.longContextEnabled = true;
  standard.longContextCostMultiplier = '3/2';

  const batch = createDefaultBillingRuleRow();
  batch.model = 'gpt-5';
  batch.serviceTier = 'batch';
  batch.baseContext = 'normal';
  batch.inputCostMultiplier = '1/2';

  const { payload, error } = parseBillingRuleRows([standard, batch]);

  assert.equal(error, undefined);
  assert.deepEqual(payload.map(({ model, serviceTier, context }) => [model, serviceTier, context]), [
    ['', '', ''],
    ['', '', 'long'],
    ['gpt-5', 'batch', 'normal']
  ]);
  assert.equal(payload[0].costMultiplier, 4 / 3);
  assert.equal(payload[1].costMultiplier, 3 / 2);
  assert.equal(payload[2].inputCostMultiplier, 1 / 2);
});

test('已有普通和 Long context 规则重新合并到同一档位卡片', () => {
  const rows = buildInitialBillingRuleRows([
    { model: '', serviceTier: '', context: '', mode: 'multiplier', costMultiplier: 1.2 },
    { model: 'gpt-5', serviceTier: 'flex', context: 'long', mode: 'fixed', inputPrice: 2.5 },
    { model: 'gpt-5', serviceTier: 'flex', context: 'normal', mode: 'multiplier', costMultiplier: 0.8 }
  ]);

  assert.equal(rows.length, 2);
  assert.equal(rows[0].longContextEnabled, false);
  assert.equal(rows[1].serviceTier, 'flex');
  assert.equal(rows[1].baseContext, 'normal');
  assert.equal(rows[1].longContextEnabled, true);
  assert.equal(rows[1].longContextMode, 'fixed');
  assert.equal(rows[1].longContextInputPrice, '2.5');
});

test('历史 Long context 单条规则仍保留为开启状态的档位卡片', () => {
  const [row] = buildInitialBillingRuleRows([
    { model: '', serviceTier: '', context: 'long', mode: 'multiplier', costMultiplier: 1.5 }
  ]);

  assert.equal(row.longContextEnabled, true);
  assert.equal(row.longContextCostMultiplier, '1.5');
});

test('倍率对照使用当前组合规则的服务档位', () => {
  assert.match(editorSource, /pricingServiceTier=\{editingRow\.serviceTier\}/);
});

test('Long context 摘要换行但倍率编辑控件仍保持右对齐', () => {
  assert.match(editorSource, /order-last basis-full[\s\S]*?className="ml-auto w-36 grid-cols-2"/);
});

test('组合规则倍率摘要不使用浏览器原生悬浮提示', () => {
  assert.doesNotMatch(editorSource, /title=\{summary\}/);
  assert.doesNotMatch(editorSource, /title=\{longSummary\}/);
});

test('未指定关系的已有例外规则与新规则默认叠加，旧兜底不重复叠加', () => {
  const rows = buildInitialBillingRuleRows([
    { mode: 'multiplier', cost_multiplier: 1.5 },
    { model: 'gpt-6.1-sol', service_tier: 'fast', context: 'normal', mode: 'multiplier', cost_multiplier: 1.25 },
    { model: 'gpt-6.1-sol', service_tier: 'fast', context: 'long', mode: 'fixed', input_price: 8, group_multiplier_mode: 'override' }
  ]);
  assert.equal(rows[0].groupMultiplierMode, 'override');
  assert.equal(rows[1].groupMultiplierMode, 'inherit');
  assert.equal(rows[1].longContextGroupMultiplierMode, 'inherit');
  const saved = parseBillingRuleRows(rows).payload;
  assert.deepEqual(saved.map((rule) => rule.groupMultiplierMode), ['override', 'inherit', 'inherit']);
  const fresh = createDefaultBillingRuleRow();
  assert.equal(fresh.groupMultiplierMode, 'inherit');
  assert.equal(fresh.costMultiplier, '1');
  const fast = createBillingRuleRow({ model: 'gpt-6.1-sol', serviceTier: 'fast', costMultiplier: '5/4' });
  assert.equal(fast.groupMultiplierMode, 'inherit');
});

test('例外规则的预览和保存始终叠加分组倍率，包括旧独立标记', () => {
  const row = createBillingRuleRow({ model: 'gpt-6.1-sol', serviceTier: 'fast', costMultiplier: '5/4', inputCostMultiplier: '1/2' });
  const group = { costMultiplier: '3/2', inputCostMultiplier: '4/3' };
  let result = getBillingRuleEffectiveMultipliers(row, group);
  assert.equal(result.total, 1.875);
  assert.equal(result.inputCostMultiplier, 2 / 3);
  row.groupMultiplierMode = 'override';
  result = getBillingRuleEffectiveMultipliers(row, group);
  assert.equal(result.total, 1.875);
  assert.equal(result.inputCostMultiplier, 2 / 3);
  const fallback = createDefaultBillingRuleRow();
  assert.equal(parseBillingRuleRows([fallback, row]).payload[1].groupMultiplierMode, 'inherit');
  row.mode = 'fixed';
  row.groupMultiplierMode = 'inherit';
  result = getBillingRuleEffectiveMultipliers(row, group);
  assert.equal(result.total, 1.5);
  assert.equal(result.inputCostMultiplier, 4 / 3);
  row.longContextMode = 'multiplier';
  row.longContextGroupMultiplierMode = 'override';
  row.longContextCostMultiplier = '2';
  row.longContextEnabled = true;
  assert.equal(getBillingRuleEffectiveMultipliers(row, group, true).total, 3);
  assert.equal(parseBillingRuleRows([fallback, row]).payload[2].groupMultiplierMode, 'inherit');
});

test('隐藏的旧默认倍率与分组倍率一致时合并为一次基础倍率，修改基础倍率后仍只乘一次', () => {
  const group = { costMultiplier: '3/2' };
  const rows = buildInitialBillingRuleRows([
    { mode: 'multiplier', cost_multiplier: 1.5 },
    { model: 'gpt-6.1-sol', service_tier: 'fast', context: 'normal', mode: 'multiplier', cost_multiplier: 1.25 }
  ], group);
  const saved = parseBillingRuleRows(rows).payload;
  assert.equal(saved[0].costMultiplier, 1);
  assert.equal(saved[0].groupMultiplierMode, 'inherit');
  assert.equal(getBillingRuleEffectiveMultipliers(rows[0], group).total, 1.5);
  assert.equal(getBillingRuleEffectiveMultipliers(rows[1], group).total, 1.875);
  assert.equal(getBillingRuleEffectiveMultipliers(rows[0], { costMultiplier: 2 }).total, 2);
  assert.equal(getBillingRuleEffectiveMultipliers(rows[1], { costMultiplier: 2 }).total, 2.5);
});

test('不同于分组基础倍率的旧默认配置保留原值', () => {
  for (const legacy of [
    { mode: 'multiplier', costMultiplier: 1.8 },
    { mode: 'fixed', inputPrice: 2, outputPrice: 7 }
  ]) {
    const rows = buildInitialBillingRuleRows([legacy], { costMultiplier: 1.5 });
    const saved = parseBillingRuleRows(rows).payload[0];
    assert.equal(saved.groupMultiplierMode, 'override');
    assert.equal(saved.mode, legacy.mode);
    if (legacy.mode === 'fixed') {
      assert.equal(saved.inputPrice, 2);
      assert.equal(saved.outputPrice, 7);
    } else assert.equal(saved.costMultiplier, 1.8);
  }
});

test('只有基础倍率时可以保存组合模式，空白附加规则不能生成另一条默认规则', () => {
  const base = createDefaultBillingRuleRow();
  assert.equal(parseBillingRuleRows([base]).payload.length, 1);
  const blank = createBillingRuleRow({ context: 'normal' });
  assert.match(parseBillingRuleRows([base, blank]).error, /指定模型或服务档位/);
});
