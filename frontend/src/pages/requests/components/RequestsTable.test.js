import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import {
  countChannelSwitches,
  getEndpointChainItems,
  getRequestNetworkDisplayState,
  requestHasInterChannelTransfer,
  summarizeEndpointAttempts
} from '../utils/endpointChain.js';
import { getBillableInputTokens, getDisplayTotalTokens } from '../utils/tokenDisplay.js';

const source = fs.readFileSync(new URL('./RequestsTable.jsx', import.meta.url), 'utf8');
const detailSource = fs.readFileSync(new URL('./RequestDetailModal.jsx', import.meta.url), 'utf8');
const pageSource = fs.readFileSync(new URL('../index.jsx', import.meta.url), 'utf8');

test('request display calculations preserve retry, transfer and token semantics', () => {
  const request = {
    provider: 'codex',
    input_tokens: 100,
    cache_read_tokens: 30,
    output_tokens: 20,
    retry_count: 2,
    endpoint_chain: JSON.stringify([
      { channel: 'A', credential_group: 'default', endpoint: 'one' },
      { channel: 'A', credential_group: 'default', endpoint: 'one' },
      { channel: 'B', credential_group: 'backup', endpoint: 'two' }
    ]),
    duration_ms: 125,
    is_streaming: true
  };
  const items = getEndpointChainItems(request);
  assert.equal(requestHasInterChannelTransfer(request), true);
  assert.equal(countChannelSwitches(items), 1);
  assert.deepEqual(summarizeEndpointAttempts(request), {
    attemptCount: 3,
    finalEndpointAttempts: null,
    retryCount: 2,
    transferCount: 1,
    transferred: true,
    firstEndpoint: 'one',
    finalEndpoint: 'two'
  });
  assert.equal(getBillableInputTokens(request), 70);
  assert.equal(getDisplayTotalTokens(request), 120);
  assert.deepEqual(getRequestNetworkDisplayState(request), { durationMs: 125, isStreaming: true });
});

test('request table never exposes a horizontal scrollbar', () => {
  assert.match(source, /overflow-x-hidden/);
  assert.doesNotMatch(source, /overflow-x-auto/);
});

test('request channel failover display is data-driven for Claude and Codex', () => {
  assert.match(source, /requestHasInterChannelTransfer\(request\)/);
  assert.match(detailSource, /const showChannelInChain = requestHasInterChannelTransfer\(request\)/);
  assert.doesNotMatch(source, /provider === 'codex'\) return false/);
  assert.doesNotMatch(source, /provider \|\| 'claude'\) !== 'codex'/);
  assert.doesNotMatch(detailSource, /isClaudeRequest && chainHasMultipleChannels/);
});

test('request transfer indicators use the shared Route icon for Claude and Codex', () => {
  const routeBadgeClass = 'inline-flex items-center gap-0.5 px-1.5 py-0.5 bg-indigo-50 text-indigo-600 text-[10px] font-medium rounded-full border border-indigo-100 whitespace-nowrap';
  assert.match(source, /const channelTransferCount = countChannelSwitches\(getEndpointChainItems\(request\)\)/);
  assert.match(source, /aria-label=\{`渠道切换 \$\{channelTransferCount\} 次`\}/);
  assert.match(source, /<span>\{channelTransferCount\}<\/span>/);
  assert.doesNotMatch(source, /<Tooltip content="渠道间转移记录">/);
  assert.equal(source.split(routeBadgeClass).length - 1, 2);
  assert.match(source, /<Route className="w-2\.5 h-2\.5" \/>/);
  assert.doesNotMatch(source, /<GitBranch/);
});

test('request channel selectors hide paused channels only while provider failover is enabled', () => {
  assert.match(pageSource, /interChannelFailoverEnabled \? groups\.filter\(group => !group\.paused\) : groups/);
  assert.match(pageSource, /codexChannels\.filter\(channel => channel\.failoverEnabled !== false\)/);
  assert.match(pageSource, /getSettingsByScope\('codex', 'failover'\)/);
  assert.match(pageSource, /visibleClaudeGroups\.map\(g => \(\{ \.\.\.g, provider: 'claude' \}\)\)/);
  assert.match(pageSource, /const codexNames = visibleCodexChannels/);
});

test('request detail modal portals the full-screen backdrop and preserves page positioning', () => {
  assert.match(detailSource, /renderModalPortal\(/);
  assert.match(detailSource, /className="fixed inset-0 z-\[10000\]"/);
  assert.match(detailSource, /className="absolute inset-0 bg-slate-900\/20 backdrop-blur-sm/);
  assert.match(detailSource, /getBoundingClientRect\(\)/);
  assert.match(detailSource, /document\.documentElement\.clientWidth/);
  assert.match(pageSource, /ref=\{requestPageRef\}/);
  assert.match(pageSource, /positioningRef=\{requestPageRef\}/);
  assert.doesNotMatch(detailSource, /absolute inset-x-0 -top-6 bottom-0 z-\[10000\]/);
});

test('reasoning column keeps thinking mode distinct from effort', () => {
  // output_config.effort 是推理强度档位；thinking.type（adaptive / enabled / disabled）
  // 只是扩展思考的开关模式。两者语义不同，不能让 adaptive 冒充强度。
  assert.match(source, /const effort = reasoning\.effort \|\| '';/);
  assert.match(source, /const thinkingType = reasoning\.type \|\| '';/);
  assert.match(source, /不是推理强度/);
  // adaptive 同属开启态，配色要走 isThinkingTypeActive 而不是只判断 enabled
  assert.match(source, /isThinkingTypeActive\(thinkingType\)/);
});

test('reasoning column renders raw values without bilingual labels', () => {
  // 列表里 effort 显示 low/high，thinking 也应显示 adaptive 原值，
  // 中文解释留在 tooltip；双语拼接只保留在详情弹窗
  assert.doesNotMatch(source, /formatReasoningFieldValue/);
  assert.match(source, /label: thinkingType,/);
});

test('billing detail reads the persisted snapshot and only reverse-derives for legacy rows', () => {
  // 有计费快照就正向渲染（落库单价 × 合并倍率）；除法反推会把 long context
  // 倍率之类未单独落库的层默默吸收进「基础单价」，只能留给无快照的旧记录
  assert.match(detailSource, /const hasSnapshot = Boolean\(snapshot\?\.context_mode\)/);
  assert.match(detailSource, /return hasSnapshot \? snapPrice\(snapshot\[snapshotField\]\) : legacyPrice\(\)/);
  assert.match(detailSource, /\? tokens \* price \/ 1_000_000 \* safeMultiplier\(snapshot\[snapshotField\]\)/);
  assert.match(detailSource, /: removeTotalMultiplier\(storedCost\)/);
  // 反推只能出现在 legacy 分支里，不得再有直接调用
  assert.doesNotMatch(detailSource, /cost: removeTotalMultiplier\(/);
  assert.doesNotMatch(detailSource, /const \w+BasePrice = calcBasePrice\(/);
  // 快照字段沿用后端命名，旧的 selector_/effective_ 结构已废弃
  assert.match(detailSource, /snapshot\.rule_mode === 'fixed'/);
  assert.doesNotMatch(detailSource, /billingRuleSnapshot\.(mode|selector_|effective_)/);
});

test('request detail keeps cancellation out of the red error card and uses one failure set', () => {
  // 取消不是错误：FAILURE_STATUSES 不含 cancelled，取消走独立的中性卡片
  assert.doesNotMatch(detailSource.match(/const FAILURE_STATUSES = new Set\(\[([\s\S]*?)\]\)/)?.[1] || '', /'cancelled'/);
  assert.match(detailSource, /\{isCancelled && \(/);
  assert.match(detailSource, /\{isFailure && \(/);
  // 错误卡片不再维护第二份手写状态列表
  assert.doesNotMatch(detailSource, /\['failed', 'error', 'cancelled', 'timeout', 'partial_failed'\]\.includes/);
  // 全页统一"流式 / 非流式"
  assert.doesNotMatch(detailSource, /常规请求/);
});

test('request detail tabs flag empty privacy and chain content', () => {
  assert.match(detailSource, /empty: !privacyRedacted/);
  assert.match(detailSource, /empty: chainNodes\.length === 0/);
});

test('request detail chain cards surface per-hop attempts and upstream status', () => {
  assert.match(detailSource, /node\.durationMs != null &&/);
  assert.match(detailSource, /node\.attempts > 1 &&/);
  assert.match(detailSource, /node\.statusCode > 0 &&/);
  // 链路徽章不再使用难以辨认的 9px 字号
  assert.doesNotMatch(detailSource, /text-\[9px\]/);
});

test('upstream error hops collapse details and stay on the rose card tone', () => {
  const hopSource = detailSource.match(/const UpstreamErrorHop = [\s\S]*?\n\};/)?.[0] || '';
  // 每跳详情默认收起，可手动切换
  assert.match(hopSource, /const \[expanded, setExpanded\] = useState\(false\)/);
  assert.match(hopSource, /aria-expanded=\{expanded\}/);
  assert.match(hopSource, /收起详情/);
  // 嵌在红色错误卡片内：不套白底卡片和边框，正文用深色代码块
  assert.doesNotMatch(hopSource, /bg-white|rounded-lg border/);
  assert.match(hopSource, /<pre className="[^"]*bg-slate-800/);
  // 转移产生的早先失败跳默认折叠，只列最终跳
  assert.match(detailSource, /showEarlierHops \? failedHops : failedHops\.slice\(0, 1\)/);
});
