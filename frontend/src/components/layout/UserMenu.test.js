import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';

const source = fs.readFileSync(new URL('./UserMenu.jsx', import.meta.url), 'utf8');

test('directory menu items keep hover feedback without movement animation', () => {
  const actionBlock = source.match(/\{\/\* 操作按钮 \*\/\}([\s\S]*?)\{\/\* 退出按钮 \*\/\}/)?.[1] || '';
  assert.equal((actionBlock.match(/transition-colors duration-150/g) || []).length, 4);
  assert.doesNotMatch(actionBlock, /hover:translate|hover:scale/);
});
