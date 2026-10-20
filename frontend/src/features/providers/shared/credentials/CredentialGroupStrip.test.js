import test from 'node:test';
import assert from 'node:assert/strict';

const read = (name) => import('node:fs').then(({ readFileSync }) => readFileSync(new URL(name, import.meta.url), 'utf8'));
const source = await read('./CredentialGroupStrip.jsx');
const statsSource = await read('./credentialGroupStats.js');

test('every group stays visible and one click selects it', () => {
  // 废弃堆叠的核心理由：选分组不该先花一次点击展开
  assert.match(source, /groups\.map\(\(group\) => \(\s*<GroupSegment/);
  assert.match(source, /onClick=\{\(\) => onSelect\?\.\(getGroupID\(group\)\)\}/);
  assert.match(source, /aria-pressed=\{selected\}/);
  // 双击查看详情保留
  assert.match(source, /onDoubleClick=\{\(\) => onOpen\?\.\(group\)\}/);
});

test('the overlay, the portal and their timers are gone', () => {
  // 常驻分段不需要浮层，连带 anchor/入场守卫/滚动收起全部消失
  assert.doesNotMatch(source, /renderModalPortal/);
  assert.doesNotMatch(source, /getBoundingClientRect/);
  assert.doesNotMatch(source, /setTimeout/);
  assert.doesNotMatch(source, /addEventListener/);
  assert.doesNotMatch(source, /useState|useEffect|useRef/);
});

test('the segment stays one line and the name gets a line box tall enough for CJK', () => {
  // leading-none 下 11px 的中文会被裁掉底部笔画，行盒必须显式给足
  assert.match(source, /const SEGMENT_AREA_HEIGHT_PX = 32;/);
  assert.match(source, /flex h-full w-full items-center gap-1 rounded-lg border px-2/);
  assert.match(source, /whitespace-nowrap text-\[11px\] font-medium leading-\[16px\]/);
  assert.match(source, /text-\[9px\] font-medium leading-\[14px\]/);
  assert.doesNotMatch(source, /className=[^\n]*leading-none/);
});

test('endpoint failover detail lives in the hover bubble, not in the segment', () => {
  // 段内不再放点阵；气泡逐个列出端点及其状态
  assert.doesNotMatch(source, /FailoverIndicator/);
  assert.match(source, /stat\.endpoints\.slice\(0, BUBBLE_ENDPOINT_LIMIT\)\.map/);
  assert.match(source, /endpoint\.failover \? 'bg-indigo-400' : 'border border-slate-500'/);
  assert.match(source, /!endpoint\.failover && \(/);
  // 端点多到列不完时报剩余个数，不静默截断
  assert.match(source, /const BUBBLE_ENDPOINT_LIMIT = 8;/);
  assert.match(source, /另 \{stat\.endpoints\.length - BUBBLE_ENDPOINT_LIMIT\} 个/);
});

test('the segment only shows a status dot when some endpoint opts out of failover', () => {
  // 正常态不占宽度；异常态不必悬浮就能看见
  assert.match(source, /const excludedCount = stat\.total - stat\.failover;/);
  assert.match(source, /\{excludedCount > 0 && \(/);
  assert.match(source, /stat\.failover === 0 \? 'bg-slate-400' : 'bg-amber-500'/);
});

test('endpoint ownership matches the channel card group filter', () => {
  // 指示器和下方端点列表若用不同规则分组，数字就会对不上
  assert.match(statsSource, /if \(endpointGroupID > 0\) return endpointGroupID === getGroupID\(group\);/);
  assert.match(statsSource, /return isDefaultGroup\(group\);/);
  // 历史数据里 failoverEnabled 出现过 '0' 字符串，只判 !== false 会漏
  assert.match(statsSource, /value === false \|\| value === 0 \|\| value === '0'/);
});

test('buildCredentialGroupStats counts what the strip renders', async () => {
  const { buildCredentialGroupStats } = await import('./credentialGroupStats.js');
  const stats = buildCredentialGroupStats(
    [{ id: 1, isDefault: true }, { id: 2 }],
    [
      { name: 'a', credentialGroupId: 1 },                        // 缺省 = 参与
      { name: 'c', credential_group_id: 1, failoverEnabled: '0' },// 字符串 '0' 也是不参与
      { name: 'b', credentialGroupId: 0 },                        // 未指定 → 归默认分组
      { name: 'd', credentialGroupId: 2, failover_enabled: false },
      { name: 'e', credentialGroupId: 9 }                         // 指向已删分组，不归任何一组
    ]
  );
  assert.equal(stats[1].total, 3);
  assert.equal(stats[1].failover, 2);
  // 不参与的排在后面，气泡清单和上方计数才是同一个顺序
  assert.deepEqual(stats[1].endpoints, [
    { name: 'a', failover: true },
    { name: 'b', failover: true },
    { name: 'c', failover: false }
  ]);
  assert.equal(stats[2].total, 1);
  assert.equal(stats[2].failover, 0);
});

test('per-group actions move into the header and target the selected group', () => {
  // 分段宽度放不下行内按钮；选中只要一次点击，先选后操作不比原先多花步骤
  assert.match(source, /onClick=\{\(\) => onSetDefault\?\.\(selectedGroup\)\}/);
  assert.match(source, /onClick=\{\(\) => onEdit\?\.\(selectedGroup\)\}/);
  assert.match(source, /onClick=\{\(\) => onDelete\?\.\(selectedGroup\)\}/);
  // 默认分组不能删，也不用再设为默认
  assert.match(source, /\{!selectedIsDefault && \(/);
});

test('group action buttons meet the minimum touch target size', () => {
  // WCAG 2.5.8 要求 24×24
  assert.match(source, /h-6 w-6/);
});

test('the strip area height never depends on the group count', () => {
  // 并排的渠道卡片底边要对得齐
  assert.match(source, /style=\{\{ height: `\$\{SEGMENT_AREA_HEIGHT_PX\}px` \}\}/);
  // 空态同样占满，否则没有分组的渠道又矮一截
  assert.match(source, /空态也占满分段区高度/);
});

test('too many groups overflow sideways instead of wrapping, growing or squeezing names', () => {
  // 换行会改变高度；滚动条也会从定高里扣掉几像素，所以整条隐藏
  assert.match(source, /no-scrollbar flex min-w-0 items-stretch gap-1 overflow-x-auto/);
  assert.doesNotMatch(source, /flex-wrap/);
  assert.match(source, /const SEGMENT_MIN_WIDTH_PX = 76;/);
  assert.match(source, /minWidth: `\$\{SEGMENT_MIN_WIDTH_PX\}px`/);
  // 裸 flex-1 是 basis:0 + shrink:1：分段会被压到名字以下，带「默认」徽章的分组
  // 先被截断，其他分组却完整。配上 min-w-max 后两种情形都对——宽度够时仍是等宽
  // 平分，不够时每段退回名字所需宽度、由上面的 overflow-x-auto 横向滚动。
  assert.match(source, /className="flex min-w-max flex-1"/);
  assert.doesNotMatch(source, /className="flex flex-1"/);
});

test('the dot strip has a text equivalent for screen readers', () => {
  // 点阵是非文本内容，摘要挂在分段上，指示器本身不重复播报
  assert.match(source, /aria-label=\{`\$\{name\}\$\{groupIsDefault \? '（默认分组）' : ''\}，\$\{summary\}`\}/);
  assert.match(source, /const buildStatSummary =/);
  assert.match(source, /aria-hidden="true"/);
});
