import test from 'node:test';
import assert from 'node:assert/strict';

const source = await import('node:fs').then(({ readFileSync }) => readFileSync(new URL('./CredentialGroupStack.jsx', import.meta.url), 'utf8'));

test('credential group stack keeps the selected card at the front', () => {
  // 后层卡片内容被淡出、不可读；选中项若不置顶，折叠态露出的就不是用户选中的那个
  assert.match(source, /const orderedGroups = useMemo/);
  assert.match(source, /return \[\.\.\.selected, \.\.\.rest\];/);
});

test('collapsed layers leave both the tab order and the accessibility tree', () => {
  // 仅靠 opacity:0 仍可被 Tab 聚焦到，必须同时设置 inert
  assert.match(source, /inert=\{isDecorative\}/);
  assert.match(source, /aria-hidden=\{isDecorative/);
  assert.match(source, /const isDecorative = !isExpanded && !isFront;/);
});

test('expand toggle exposes aria-expanded and the controlled list', () => {
  assert.match(source, /aria-expanded=\{isExpanded\}/);
  assert.match(source, /aria-controls=\{listId\}/);
});

test('group action buttons meet the minimum touch target size', () => {
  // 原胶囊内按钮实际点击区约 14×14，低于 WCAG 2.5.8 要求的 24×24
  assert.match(source, /h-6 w-6/);
});

test('stack preserves single-click switching and double-click details', () => {
  assert.match(source, /单击切换，双击查看详情/);
  assert.match(source, /onDoubleClick=\{\(\) => onOpen\?\.\(group\)\}/);
  // 操作按钮上的双击不应冒泡成「查看详情」
  assert.match(source, /onDoubleClick=\{\(event\) => event\.stopPropagation\(\)\}/);
});

test('stack falls back to an internal scroll instead of growing without bound', () => {
  // 分组数量在不同渠道差别很大，展开后必须限高，否则会把渠道卡片推得过长
  assert.match(source, /SCROLL_THRESHOLD/);
  assert.match(source, /overflow-y-auto/);
});
