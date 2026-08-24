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
  assert.match(source, /const isDecorative = !isFront;/);
});

test('expanding renders a portal overlay so the channel card never grows', () => {
  // ChannelCard 根元素带 overflow-hidden，就地展开会被裁剪；
  // 撑开又会改变渠道卡片高度。因此走 portal + fixed 浮层。
  assert.match(source, /createPortal\(/);
  assert.match(source, /document\.body/);
  assert.match(source, /fixed z-\[80\]/);
  // 折叠堆叠常驻并保持固定高度，兼作浮层锚点
  assert.match(source, /style=\{\{ height: `\$\{collapsedHeight\}px` \}\}/);
});

test('double-clicking a collapsed card expands instead of opening details', () => {
  assert.match(source, /const handleCardDoubleClick = useCallback/);
  assert.match(source, /if \(!isExpanded && canExpand\) \{\s*openExpanded\(\);/);
  // 展开后（或只有一个分组时）双击仍是查看详情
  assert.match(source, /onOpen\?\.\(group\);/);
  assert.match(source, /const doubleClickHint = canExpand && !isExpanded \? '单击切换，双击展开全部' : '单击切换，双击查看详情';/);
});

test('overlay stays anchored or closes rather than drifting', () => {
  // fixed 定位用的是视口坐标，滚动/缩放后锚点失效，直接收起
  assert.match(source, /window\.addEventListener\('scroll', closeExpanded, true\)/);
  assert.match(source, /window\.addEventListener\('resize', closeExpanded\)/);
  // 视口下方空间不足时向上浮出，避免被截断
  assert.match(source, /MIN_DROP_DOWN_SPACE_PX/);
  assert.match(source, /const dropUp =/);
});

test('overlay can be dismissed by keyboard and outside click', () => {
  assert.match(source, /event\.key === 'Escape'/);
  assert.match(source, /popoverRef\.current\?\.contains\(event\.target\)/);
});

test('expand toggle exposes aria-expanded and the controlled list', () => {
  assert.match(source, /aria-expanded=\{isExpanded\}/);
  assert.match(source, /aria-controls=\{listId\}/);
});

test('group action buttons meet the minimum touch target size', () => {
  // 原胶囊内按钮实际点击区约 14×14，低于 WCAG 2.5.8 要求的 24×24
  assert.match(source, /h-6 w-6/);
});

test('action buttons do not bubble a double click into the card handler', () => {
  assert.match(source, /onDoubleClick=\{\(event\) => event\.stopPropagation\(\)\}/);
});

test('overlay falls back to an internal scroll instead of growing without bound', () => {
  // 分组数量在不同渠道差别很大，浮层必须限高
  assert.match(source, /SCROLL_THRESHOLD/);
  assert.match(source, /overflow-y-auto/);
  assert.match(source, /maxHeight: `min\(15rem, \$\{anchor\.maxHeight\}px\)`/);
});
