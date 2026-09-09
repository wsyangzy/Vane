import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

import {
  buildInitialBillingRuleRows,
  createDefaultBillingRuleRow,
  parseBillingRuleRows
} from './billingRuleRows.js';

const editorSource = readFileSync(new URL('./BillingRuleEditor.jsx', import.meta.url), 'utf8');

test('组合规则用 Long context 开关替代上下文下拉选择', () => {
  assert.match(editorSource, /role="switch"/);
  assert.match(editorSource, /createBillingRuleRow\(\{ context: 'normal' \}\)/);
  assert.match(editorSource, /MultiplierSettingsModal/);
  assert.match(editorSource, /defaultRule && <span[^>]*>默认规则<\/span>/);
  assert.match(editorSource, /if \(isCatchAll\(row\)\) return 'Standard'/);
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
