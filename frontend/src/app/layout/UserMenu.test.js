import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';

const source = fs.readFileSync(new URL('./UserMenu.jsx', import.meta.url), 'utf8');

test('directory menu items keep hover feedback without movement animation', () => {
  const actionBlock = source.match(/\{\/\* 操作按钮 \*\/\}([\s\S]*?)\{\/\* 退出按钮 \*\/\}/)?.[1] || '';
  assert.equal((actionBlock.match(/transition-colors duration-150/g) || []).length, 4);
  assert.doesNotMatch(actionBlock, /hover:translate|hover:scale/);
});

test('program version is clickable and replays the archived current release notes', () => {
  assert.match(source, /getReleaseAnnouncement\(systemInfo\?\.version, releaseNotes\)/);
  assert.match(source, /showReleaseToastSummary\(\{/);
  assert.match(source, /showToast: toast\.show/);
  assert.match(source, /dismissToast: toast\.dismiss/);
  assert.match(source, /onValueClick=\{systemInfo\?\.version \? handleShowReleaseNotes : undefined\}/);
  assert.match(source, /title="查看当前版本更新内容"/);
  assert.match(source, /<ChevronRight size=\{12\}/);
  assert.doesNotMatch(source, /playReleaseToastSequence\(/);
});
