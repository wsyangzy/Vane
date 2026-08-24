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

test('stack area height never depends on the group count', () => {
  // 「有堆叠」与「只有一个分组」若高度不同，并排的渠道卡片底边会对不齐
  assert.match(source, /const STACK_AREA_HEIGHT_PX = CARD_HEIGHT_PX \+ \(MAX_STACK_LAYERS - 1\) \* STACK_OFFSET_PX;/);
  assert.match(source, /style=\{\{ height: `\$\{STACK_AREA_HEIGHT_PX\}px` \}\}/);
  // 空态同样占满，否则没有分组的渠道又矮一截
  assert.match(source, /空态也占满堆叠区高度/);
});

test('the separate expand button is gone and its role moves onto the card', () => {
  // 单击卡片即弹窗，再留一个展开按钮就是重复入口
  assert.doesNotMatch(source, /展开 \$\{/);
  assert.doesNotMatch(source, /ChevronDown/);
  // 弹窗语义必须跟着走，否则键盘与读屏用户失去入口
  assert.match(source, /aria-haspopup=\{dialogTrigger \? 'dialog' : undefined\}/);
  assert.match(source, /aria-expanded=\{dialogTrigger \? expanded : undefined\}/);
  // 同一个按钮不能既是 disclosure 又是 toggle
  assert.match(source, /aria-pressed=\{dialogTrigger \? undefined : selected\}/);
});

test('add-group button sits in the header row', () => {
  assert.match(source, /const header = \(/);
  assert.match(source, /<AddGroupButton onAdd=\{onAdd\} disabled=\{disabled\} \/>/);
});

test('clicking a collapsed card opens the dialog instead of re-selecting', () => {
  // 折叠态可见的只有选中项，对它「单击切换」无从谈起，所以单击留给弹窗
  assert.match(source, /onActivate=\{canExpand \? openDialog : undefined\}/);
  assert.match(source, /hint=\{canExpand \? '单击选择分组' : '双击查看详情'\}/);
});

test('groups are listed in a centred modal, not an anchored popover', () => {
  // 锚定浮层在渠道卡片密集排布时会压到相邻卡片，也受视口边缘影响
  assert.match(source, /renderModalPortal\(/);
  assert.match(source, /fixed inset-0 z-\[100\] flex items-center justify-center/);
  assert.match(source, /role="dialog"/);
  assert.match(source, /aria-modal="true"/);
  assert.match(source, /aria-labelledby=\{titleId\}/);
  // 定位相关的锚点计算已随之删除
  assert.doesNotMatch(source, /getBoundingClientRect/);
  assert.doesNotMatch(source, /dropUp/);
});

test('picking a group inside the dialog selects it and closes', () => {
  assert.match(source, /onActivate=\{handleSelect\}/);
  assert.match(source, /onSelect\?\.\(Number\(group\.id\) \|\| 0\);\s*closeDialog\(\);/);
  // 弹窗内保留双击查看详情
  assert.match(source, /onDoubleClickCard=\{onOpen\}/);
});

test('dialog cards fade in one after another', () => {
  // 挂载后下一帧才切到进入态，否则没有起始状态可过渡
  assert.match(source, /requestAnimationFrame\(\(\) => setEntered\(true\)\)/);
  assert.match(source, /transitionDelay: entered \? `\$\{Math\.min\(index, 8\) \* 20\}ms` : '0ms'/);
});

test('dialog ignores clicks while it is still animating in', () => {
  // 双击折叠卡片时第一次点击弹出弹窗，第二次会落在遮罩上立刻关闭、或落在卡片上
  // 误触选中；入场期间不吃点击，穿透到下方已 inert 的堆叠，等于无操作
  assert.match(source, /const OPEN_GUARD_MS = 220;/);
  assert.match(source, /setTimeout\(\(\) => setInteractive\(true\), OPEN_GUARD_MS\)/);
  assert.match(source, /\$\{\s*interactive \? '' : 'pointer-events-none'\s*\}/);
});

test('dialog can be dismissed by keyboard, backdrop and close button', () => {
  assert.match(source, /event\.key === 'Escape'/);
  // 只有点在遮罩本身上才关闭，点弹窗内部不应关
  assert.match(source, /event\.target === event\.currentTarget\) closeDialog\(\)/);
  assert.match(source, /onMouseDown=\{\(event\) => event\.stopPropagation\(\)\}/);
  assert.match(source, /aria-label="关闭"/);
});

test('group action buttons meet the minimum touch target size', () => {
  // 原胶囊内按钮实际点击区约 14×14，低于 WCAG 2.5.8 要求的 24×24
  assert.match(source, /h-6 w-6/);
});

test('action buttons never bubble into the card-wide click target', () => {
  // 整卡可点后，编辑/删除若不阻止冒泡就会顺带打开弹窗或选中
  assert.match(source, /const stopBubble = \(handler\) => \(event\) => \{\s*event\.stopPropagation\(\);/);
  assert.match(source, /onClick=\{stopBubble\(onEdit\)\}/);
  assert.match(source, /onClick=\{stopBubble\(onDelete\)\}/);
  assert.match(source, /onDoubleClick=\{\(event\) => event\.stopPropagation\(\)\}/);
});

test('dialog body scrolls instead of growing past the viewport', () => {
  assert.match(source, /max-h-\[calc\(100vh-2rem\)\]/);
  assert.match(source, /overflow-y-auto/);
});
