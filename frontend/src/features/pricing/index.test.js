import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';

const source = fs.readFileSync(new URL('./index.jsx', import.meta.url), 'utf8');
const styles = fs.readFileSync(new URL('./pricing.css', import.meta.url), 'utf8');

test('pricing form gives每个档位独立一页，而不是让模型单选一个档位', () => {
  assert.match(source, /key:\s*'basic',\s*label:\s*'基础信息'/);
  assert.match(source, /const tierTabKey = \(tier\) => `tier:\$\{tier\}`/);
  // 每个档位一页，共用同一套价格字段组件
  assert.match(source, /<TierPriceFields/);
  assert.match(source, /values=\{values\}/);
  assert.match(source, /errors=\{errors\.tiers\?\.\[tab\.tier\] \|\| \{\}\}/);
  // 单选档位的下拉必须消失——它正是「一个模型只能配一个档位」的来源
  assert.doesNotMatch(source, /serviceTier'\s*,\s*value\)/);
  assert.doesNotMatch(source, /activeTab\s*===\s*'standard'/);
  assert.doesNotMatch(source, /activeTab\s*===\s*'longContext'/);
});

test('pricing dialogs escape the app stacking context and constrain scrolling to the form body', () => {
  assert.match(source, /renderModalPortal/);
  assert.equal((source.match(/\{ root: true \}/g) || []).length, 2);
  assert.match(styles, /\.pricing-modal-layer\s*\{[^}]*position: fixed;[^}]*z-index: 10000;/);
  assert.match(styles, /\.pricing-form-dialog\s*\{[^}]*100dvh/);
  assert.match(styles, /\.pricing-form-body\s*\{[^}]*min-height: 0;[^}]*overflow-y: auto/);
  assert.match(styles, /\.pricing-form-footer\s*\{[^}]*flex-shrink: 0/);
  assert.doesNotMatch(source, /pt-\[10vh\]/);
});

test('档位 tab 与 long context 定价仅对 Codex 展示', () => {
  assert.match(source, /const showTierTabs = formData\.provider === 'codex'/);
  assert.match(source, /visibleFormTabs/);
  // Tailwind JIT 不解析拼接类名，列数只能查表
  assert.match(source, /TAB_GRID_COLS\[visibleFormTabs\.length\]/);
  // 切回 Claude 时需离开已隐藏的 tab，否则表单会停在空白页
  assert.match(source, /handleProviderChange/);
  assert.match(source, /setActiveTab\(tierTabKey\(''\)\)/);
  // 校验也要跳过，否则历史脏数据会把 activeTab 推到不存在的 tab 导致无法提交
  assert.match(source, /if \(provider === 'codex'\) \{[\s\S]*?longContextThreshold/);
});

test('列表按模型聚合渲染，React key 带 provider 前缀', () => {
  assert.match(source, /visiblePricingGroups\.map\(\(group\) => \(/);
  assert.match(source, /key=\{group\.key\}/);
  assert.match(source, /group=\{group\}/);
});
