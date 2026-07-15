import test from 'node:test';
import assert from 'node:assert/strict';

const source = await import('node:fs').then(({ readFileSync }) => readFileSync(new URL('./index.jsx', import.meta.url), 'utf8'));

test('credential-group billing separates standard and conditional long-context multipliers', () => {
  assert.match(source, /role="switch"/);
  assert.match(source, /普通成本倍率/);
  assert.match(source, /LongContextMultiplierSettings/);
  assert.match(source, /Long context 倍率已配置/);
  assert.match(source, /setLongContextSettingsOpen\(true\)/);
});

test('new credential groups disable long-context pricing and endpoint details show enabled multipliers', () => {
  assert.match(source, /disableLongContextPricing:\s*group\s*\?\s*Boolean\([\s\S]*?\)\s*:\s*true/);
  assert.match(source, /Long context 分组倍率/);
  assert.match(source, /longContextPricingEnabled\s*&&/);
});

test('Claude credential group and endpoint controls match Codex current-state icons', () => {
  assert.match(source, /const RepeatOffIcon/);
  assert.match(source, /const ENDPOINT_STATE_BUTTON_CLASS/);
  assert.match(source, /onSetGroupEndpointSkipHealthCheck/);
  assert.match(source, /handleSetGroupEndpointSkipHealthCheck/);
  assert.match(source, /groupSkippedHealthEndpoints/);
  assert.match(source, /groupCheckedHealthEndpoints/);
  assert.match(source, /当前分组存在正常检测的端点/);
  assert.match(source, /当前分组端点全部跳过检测/);
  assert.match(source, /failoverEnabled\s*\?\s*<Repeat size=\{12\} \/>\s*:\s*<RepeatOffIcon size=\{12\} \/>/);
  assert.match(source, /isSkipHealthCheck\s*\?\s*<ShieldOff size=\{12\} \/>\s*:\s*<ShieldCheck size=\{12\} \/>/);
  assert.match(source, /isSkipHealthCheck\s*\?\s*'text-slate-400 hover:bg-slate-100 hover:text-emerald-600'\s*:\s*'bg-emerald-50 text-emerald-600 hover:bg-emerald-100'/);
  assert.match(source, /shouldEnableGroupFailover\s*\?\s*<RepeatOffIcon size=\{12\} \/>\s*:\s*<Repeat size=\{12\} \/>/);
  assert.match(source, /shouldSkipGroupHealthCheck\s*\?\s*<ShieldCheck size=\{12\} \/>\s*:\s*<ShieldOff size=\{12\} \/>/);
});

test('Claude failover status displays use repeat icons instead of the legacy route-switch icon', () => {
  assert.match(source, /statusBadgeText === '不参与' && <RepeatOffIcon size=\{10\}/);
  assert.match(source, /icon=\{failoverEnabled \? Repeat : RepeatOffIcon\}/);
  assert.match(source, /failoverEnabled \? '参与故障转移' : '不参与转移'/);
  assert.doesNotMatch(source, /DetailPill[^>]*icon=\{ArrowRightLeft\}/);
});

test('Claude failover action hover targets keep a fixed square size', () => {
  const stateButtonUses = source.match(/\$\{ENDPOINT_STATE_BUTTON_CLASS\}/g) || [];
  assert.ok(stateButtonUses.length >= 4);
});

test('Claude channel priority and failover control match Codex status styling', () => {
  assert.match(source, /Number\.isFinite\(channelPriority\) && channelPriority >= 0/);
  assert.match(source, /initialValue\?\.priority \?\? 0/);
  assert.match(source, /min=\{0\}/);
  assert.match(source, /meta\?\.priority \?\? 0/);
  assert.doesNotMatch(source, /转移 \{priorityLabel\}/);
  assert.match(source, /当前接管/);
  assert.match(source, /已中断/);
  assert.match(source, /等待重新激活/);
  assert.match(source, /当前渠道参与渠道间转移，点击暂停/);
  assert.match(source, /当前渠道已暂停渠道间转移，点击恢复/);
  const channelStateButtons = source.match(/inline-flex size-\[26px\]/g) || [];
  assert.equal(channelStateButtons.length, 2);
});

test('Claude channel cards keep the paused badge in the credentials header', () => {
  assert.doesNotMatch(source, /按请求模型/);
  assert.doesNotMatch(source, /modelMappingCount/);
  assert.match(source, /Credentials · \{credentialGroups\.length\}[\s\S]*?channelFailoverEnabled && isPaused/);
  assert.match(source, /inline-flex h-4 flex-shrink-0 items-center rounded bg-amber-50/);
});

test('Claude channel cards align when collapsed without stretching expanded siblings', () => {
  assert.match(source, /grid grid-cols-1 items-start gap-4 lg:grid-cols-2 xl:grid-cols-3/);
  assert.doesNotMatch(source, /grid grid-cols-1 items-stretch gap-4 lg:grid-cols-2 xl:grid-cols-3/);
});

test('Claude endpoint rows keep one fixed height for every failover state', () => {
  assert.match(source, /className=\{`group\/row relative flex h-\[56px\] min-h-\[56px\] max-h-\[56px\] items-start/);
  assert.doesNotMatch(source, /className=\{`group\/row relative flex min-h-\[56px\] items-start/);
});

test('Claude endpoint section header keeps a fixed height across group states', () => {
  assert.match(source, /className="flex h-9 items-center justify-between px-4"/);
  assert.doesNotMatch(source, /className="flex items-center justify-between px-4 py-1\.5"/);
});

test('Claude KPI switches between failover scope and active-channel scope', () => {
  const failoverBlock = source.match(/const failoverOverviewCards = \[([\s\S]*?)\n  \];\n  const activeChannelOverviewCards/)?.[1] || '';
  const failoverTitles = ['参与端点', '健康端点', '跳过检测', '异常端点', '未参与端点', '参与渠道'];
  let previous = -1;
  for (const title of failoverTitles) {
    const index = failoverBlock.indexOf(`title: '${title}'`);
    assert.ok(index > previous, `${title} should follow the previous failover KPI card`);
    previous = index;
  }

  const activeBlock = source.match(/const activeChannelOverviewCards = \[([\s\S]*?)\n  \];\n  const channelOverviewCards/)?.[1] || '';
  const activeTitles = ['凭证分组', '参与端点', '健康端点', '跳过检测', '异常端点', '总端点'];
  previous = -1;
  for (const title of activeTitles) {
    const index = activeBlock.indexOf(`title: '${title}'`);
    assert.ok(index > previous, `${title} should follow the previous active-channel KPI card`);
    previous = index;
  }
  assert.match(source, /const channelOverviewCards = channelFailoverEnabled \? failoverOverviewCards : activeChannelOverviewCards/);
  assert.doesNotMatch(source, /scopedChannelCount/);
});

test('Claude refresh replaces the standalone check-all action', () => {
  assert.match(source, /const handleRefresh = async \(\) =>/);
  assert.match(source, /await performBatchHealthCheckAll\(\)/);
  assert.match(source, /onClick=\{handleRefresh\}/);
  assert.doesNotMatch(source, /检测全部/);
  assert.doesNotMatch(source, /icon=\{Activity\}/);
});

test('Claude header shows the SQLite channel refresh time', () => {
  assert.match(source, /const \[channelsLastUpdate, setChannelsLastUpdate\] = useState\('\'\);/);
  assert.match(source, /setChannelsLastUpdate\(new Date\(\)\.toLocaleTimeString\(\)\)/);
  assert.match(source, /const headerLastUpdate = isSqliteMode \? channelsLastUpdate : lastUpdate/);
  assert.match(source, /headerLastUpdate &&/);
});

test('Claude header does not show the SSE realtime badge', () => {
  assert.doesNotMatch(source, /SSE 状态指示器/);
  assert.doesNotMatch(source, /sseConnectionStatus/);
});
