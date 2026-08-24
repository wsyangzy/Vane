import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';

const source = fs.readFileSync(new URL('./ModelTag.jsx', import.meta.url), 'utf8');

test('model tag keeps the full name and rewrite indicator on one line', () => {
  assert.match(source, /whitespace-nowrap/);
  assert.match(source, /rewriteIndicator\s*&&/);
  assert.match(source, /shrink-0/);
  assert.doesNotMatch(source, /truncate/);
  assert.doesNotMatch(source, /left-full/);
});

test('gpt-5.6 codename variants get distinct colors', () => {
  // sol / terra / luna 同属 5.6，此前共用 minor 配色而无法区分
  assert.match(source, /GPT5_CODENAME_COLOR_MAP/);

  // 代号判定必须早于 minor 配色，否则三者仍会一起落到 5.6 的配色上
  const codenameIdx = source.indexOf('GPT5_CODENAME_COLOR_MAP[codename]');
  const minorIdx = source.indexOf('GPT5_MINOR_COLOR_MAP[minorVersion]');
  assert.ok(codenameIdx > 0, 'codename lookup must exist');
  assert.ok(minorIdx > 0, 'minor lookup must exist');
  assert.ok(codenameIdx < minorIdx, 'codename lookup must precede the minor lookup');

  // 三个代号各自定义了配色，且互不相同
  const mapBlock = source.slice(source.indexOf('const GPT5_CODENAME_COLOR_MAP'));
  const styleLines = ['sol', 'terra', 'luna'].map((name) => {
    const line = mapBlock
      .split('\n')
      .find((text) => text.trimStart().startsWith(`${name}:`));
    assert.ok(line, `${name} must have its own color entry`);
    return line.trim();
  });
  assert.equal(new Set(styleLines).size, 3, 'sol / terra / luna must not share a color');
});
