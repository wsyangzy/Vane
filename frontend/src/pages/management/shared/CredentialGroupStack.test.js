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
  // 单击卡片即展开，再留一个展开按钮就是重复入口
  assert.doesNotMatch(source, /展开 \$\{/);
  assert.doesNotMatch(source, /ChevronDown/);
  // disclosure 语义必须跟着走，否则键盘与读屏用户失去展开入口
  assert.match(source, /aria-expanded=\{expandTrigger \? expanded : undefined\}/);
  assert.match(source, /aria-controls=\{expandTrigger \? controlsId : undefined\}/);
  // 同一个按钮不能既是 disclosure 又是 toggle
  assert.match(source, /aria-pressed=\{expandTrigger \? undefined : selected\}/);
});

test('add-group button sits in the header row', () => {
  assert.match(source, /const header = \(/);
  assert.match(source, /<AddGroupButton onAdd=\{onAdd\} disabled=\{disabled\} \/>/);
});

test('clicking a collapsed card opens the overlay instead of re-selecting', () => {
  // 折叠态可见的只有选中项，对它「单击切换」无从谈起，所以单击留给展开
  assert.match(source, /onActivate=\{canExpand \? openExpanded : undefined\}/);
  assert.match(source, /hint=\{canExpand \? '单击展开全部分组' : '双击查看详情'\}/);
});

test('picking a group inside the overlay selects it and closes', () => {
  assert.match(source, /onActivate=\{handleSelect\}/);
  assert.match(source, /onSelect\?\.\(Number\(group\.id\) \|\| 0\);\s*closeExpanded\(\);/);
  // 浮层内保留双击查看详情
  assert.match(source, /onDoubleClickCard=\{onOpen\}/);
});

test('overlay floats outside the channel card rather than inside it', () => {
  // ChannelCard 根元素带 overflow-hidden，就地展开会被裁剪；撑开又会改变卡片高度
  assert.match(source, /createPortal\(/);
  assert.match(source, /document\.body/);
  assert.match(source, /fixed z-\[80\]/);
  // 左右各向外溢出，视觉上明确浮在卡片之外
  assert.match(source, /const OVERLAY_BLEED_PX = 6;/);
  assert.match(source, /left: rect\.left - OVERLAY_BLEED_PX/);
  assert.match(source, /width: rect\.width \+ OVERLAY_BLEED_PX \* 2/);
  // 抵消浮层内边距，让第一张卡片正好压在堆叠顶层原位
  assert.match(source, /rect\.top - OVERLAY_PADDING_PX/);
});

test('cards fan out from the stacked position when the overlay opens', () => {
  // 挂载后下一帧才切到展开态，否则没有起始状态可过渡
  assert.match(source, /requestAnimationFrame\(\(\) => setEntered\(true\)\)/);
  // 未进入时把每张卡按行距反推回堆叠位置
  assert.match(source, /translateY\(\$\{index \* STACK_OFFSET_PX - index \* ROW_STRIDE_PX\}px\)/);
  assert.match(source, /const ROW_STRIDE_PX = CARD_HEIGHT_PX \+ CARD_GAP_PX;/);
});

test('overlay finishes its closing animation before unmounting', () => {
  // 直接卸载 portal 会让浮层瞬间消失，看不到收起过程
  assert.match(source, /setTimeout\(\(\) => setAnchor\(null\), CLOSE_ANIMATION_MS\)/);
  assert.match(source, /clearTimeout\(closeTimerRef\.current\)/);
});

test('overlay ignores clicks while it is still animating in', () => {
  // 双击折叠卡片时第一次点击展开浮层，第二次会落在刚浮出的浮层上误触选中；
  // 动画期间不吃点击，穿透到下方已 inert 的堆叠，等于无操作
  assert.match(source, /setTimeout\(\(\) => setInteractive\(true\), 220\)/);
  assert.match(source, /\$\{interactive \? '' : 'pointer-events-none'\}/);
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

test('group action buttons meet the minimum touch target size', () => {
  // 原胶囊内按钮实际点击区约 14×14，低于 WCAG 2.5.8 要求的 24×24
  assert.match(source, /h-6 w-6/);
});

test('action buttons never bubble into the card-wide click target', () => {
  // 整卡可点后，编辑/删除若不阻止冒泡就会顺带触发展开或选中
  assert.match(source, /const stopBubble = \(handler\) => \(event\) => \{\s*event\.stopPropagation\(\);/);
  assert.match(source, /onClick=\{stopBubble\(onEdit\)\}/);
  assert.match(source, /onClick=\{stopBubble\(onDelete\)\}/);
  assert.match(source, /onDoubleClick=\{\(event\) => event\.stopPropagation\(\)\}/);
});

test('overlay falls back to an internal scroll instead of growing without bound', () => {
  // 分组数量在不同渠道差别很大，浮层必须限高
  assert.match(source, /SCROLL_THRESHOLD/);
  assert.match(source, /overflow-y-auto/);
  assert.match(source, /maxHeight: `min\(15rem, \$\{anchor\.maxHeight\}px\)`/);
});
