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
