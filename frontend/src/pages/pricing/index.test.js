import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';

const source = fs.readFileSync(new URL('./index.jsx', import.meta.url), 'utf8');

test('pricing form separates info, standard pricing and long-context settings into tabs', () => {
  assert.match(source, /key:\s*'basic'.*label:\s*'基础信息'/s);
  assert.match(source, /key:\s*'standard'.*label:\s*'普通定价'/s);
  assert.match(source, /key:\s*'longContext'.*label:\s*'Long context'/s);
  assert.match(source, /activeTab\s*===\s*'standard'/);
  assert.match(source, /activeTab\s*===\s*'longContext'/);
  assert.match(source, /setActiveTab\('standard'\)/);
});

test('pricing modal centers within the viewport without page scrolling', () => {
  assert.match(source, /fixed inset-0[^"']*items-center[^"']*overflow-hidden/);
  assert.match(source, /max-h-\[calc\(100vh-2rem\)\]/);
  assert.doesNotMatch(source, /pt-\[10vh\]/);
});

test('pricing summary cards keep the same height when default names wrap', () => {
  assert.match(source, /grid auto-rows-fr grid-cols-1[^"']*xl:grid-cols-5/);
  assert.match(source, /className="block h-full"/);
  assert.match(source, /className="h-full bg-white rounded-2xl[^"']*hover:shadow-md"/);
});

test('long-context pricing tab is scoped to the Codex provider', () => {
  // Claude 侧已移除 Long context，定价表单不应再对其展示该 tab
  assert.match(source, /const showLongContextTab = formData\.provider === 'codex'/);
  assert.match(source, /visibleFormTabs/);
  assert.match(source, /showLongContextTab \? 'grid-cols-3' : 'grid-cols-2'/);
  // 切回 Claude 时需离开已隐藏的 tab，否则表单会停在空白页
  assert.match(source, /handleProviderChange/);
  assert.match(source, /setActiveTab\('standard'\)/);
  // 校验也要跳过，否则历史脏数据会把 activeTab 推到不存在的 tab 导致无法提交
  assert.match(source, /if \(formData\.provider === 'codex'\) \{[\s\S]*?longContextThreshold/);
});
