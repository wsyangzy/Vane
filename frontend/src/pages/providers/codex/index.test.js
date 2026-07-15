import test from 'node:test';
import assert from 'node:assert/strict';

const source = await import('node:fs').then(({ readFileSync }) => readFileSync(new URL('./index.jsx', import.meta.url), 'utf8'));
const endpointBadgeSource = await import('node:fs').then(({ readFileSync }) => readFileSync(new URL('../../management/shared/endpointBadges.jsx', import.meta.url), 'utf8'));
const wailsApiSource = await import('node:fs').then(({ readFileSync }) => readFileSync(new URL('../../../utils/wailsApi.js', import.meta.url), 'utf8'));

test('credential-group long-context billing uses a switch and detailed multipliers', () => {
  assert.match(source, /role="switch"/);
  assert.match(source, /普通成本倍率/);
  assert.match(source, /LongContextMultiplierSettings/);
  assert.match(source, /Long context 倍率已配置/);
  assert.match(source, /setLongContextSettingsOpen\(true\)/);
  assert.match(source, /Long context 总倍率/);
  assert.match(source, /Long context 输入/);
  assert.match(source, /Long context 输出/);
  assert.match(source, /Long context 缓存创建/);
  assert.match(source, /Long context 缓存读取/);
});

test('new credential groups disable long-context pricing and endpoint details show enabled multipliers', () => {
  assert.match(source, /disableLongContextPricing:\s*group\s*\?\s*Boolean\([\s\S]*?\)\s*:\s*true/);
  assert.match(source, /Long context 分组倍率/);
  assert.match(source, /longContextPricingEnabled\s*&&/);
});

test('Codex global configuration leaves reasoning effort to the client', () => {
  assert.doesNotMatch(source, /updateField\('reasoning_effort'/);
  assert.doesNotMatch(source, /写入顶层 model_reasoning_effort 字段/);
  assert.doesNotMatch(source, /globalConfig\.reasoning_effort/);
  assert.doesNotMatch(source, /effective_reasoning_effort/);
});

test('Codex channel priority follows the Claude create and edit flow', () => {
  assert.match(source, /priority:\s*isEdit\s*\?\s*Math\.max\(0, Number\.parseInt\(formData\.priority, 10\) \|\| 0\)\s*:\s*-1/);
  assert.match(source, /channelFailoverEnabled && isEdit &&/);
  assert.match(source, /数字越小越优先，相同则交换/);
  assert.match(wailsApiSource, /priority:\s*Number\.parseInt\(input\?\.priority, 10\) \|\| 0/);
  assert.doesNotMatch(wailsApiSource, /priority:\s*Math\.max\(0, Number\.parseInt\(input\?\.priority/);
});

test('credential group bulk controls and endpoint rows use current-state repeat and shield icons', () => {
  assert.match(source, /onSetGroupEndpointSkipHealthCheck/);
  assert.match(source, /handleSetGroupEndpointSkipHealthCheck/);
  assert.match(source, /groupSkippedHealthEndpoints/);
  assert.match(source, /groupCheckedHealthEndpoints/);
  assert.match(source, /当前分组存在正常检测的端点/);
  assert.match(source, /当前分组端点全部跳过检测/);
  assert.match(source, /const RepeatOffIcon/);
  assert.match(source, /failoverEnabled\s*\?\s*<Repeat size=\{12\} \/>\s*:\s*<RepeatOffIcon size=\{12\} \/>/);
  assert.match(source, /skipHealthCheck\s*\?\s*<ShieldOff size=\{12\} \/>\s*:\s*<ShieldCheck size=\{12\} \/>/);
  assert.match(source, /skipHealthCheck\s*\?\s*'text-slate-400 hover:bg-slate-100 hover:text-emerald-600'\s*:\s*'bg-emerald-50 text-emerald-600 hover:bg-emerald-100'/);
  assert.match(source, /shouldEnableGroupFailover\s*\?\s*<RepeatOffIcon size=\{12\} \/>\s*:\s*<Repeat size=\{12\} \/>/);
  assert.match(source, /shouldSkipGroupHealthCheck\s*\?\s*<ShieldCheck size=\{12\} \/>\s*:\s*<ShieldOff size=\{12\} \/>/);
});

test('Codex endpoint details use repeat icons for the current failover state', () => {
  assert.match(source, /icon=\{failoverEnabled \? Repeat : RepeatOffIcon\}/);
  assert.doesNotMatch(source, /DetailPill[^>]*icon=\{ArrowRightLeft\}/);
});

test('Codex failover action hover targets keep a fixed square size', () => {
  const stateButtonUses = source.match(/\$\{ENDPOINT_STATE_BUTTON_CLASS\}/g) || [];
  assert.ok(stateButtonUses.length >= 4);
});

test('Codex keeps the selected credential group stable and uses repeat-off for excluded badges', () => {
  assert.match(source, /useState\(preferredCredentialGroupID\)/);
  assert.match(source, /Number\(group\.id\) === Number\(selectedCredentialGroupID\)/);
  assert.match(endpointBadgeSource, /excluded:\s*\['不参与', RepeatOffIcon/);
});

test('Codex channel cards align when collapsed without stretching expanded siblings', () => {
  assert.match(source, /grid grid-cols-1 items-start gap-4 md:grid-cols-2 xl:grid-cols-3/);
  assert.doesNotMatch(source, /grid grid-cols-1 items-stretch gap-4 md:grid-cols-2 xl:grid-cols-3/);
});

test('Codex endpoint rows keep one fixed height for every failover state', () => {
  assert.match(source, /className=\{`group\/row flex h-\[56px\] min-h-\[56px\] max-h-\[56px\] items-start/);
  assert.doesNotMatch(source, /className=\{`group\/row flex min-h-\[56px\] items-start/);
});

test('Codex endpoint section header keeps a fixed height across group states', () => {
  assert.match(source, /className="flex h-9 items-center justify-between px-4"/);
  assert.doesNotMatch(source, /className="flex items-center justify-between px-4 py-1\.5"/);
});

test('Codex batch failover errors use an auto-closing toast instead of the page error', () => {
  assert.match(source, /useToast/);
  assert.match(source, /toast\.error\(getFailoverToggleErrorMessage\(err, '批量操作失败'\), '批量操作失败'\)/);
  assert.doesNotMatch(source, /setError\(getFailoverToggleErrorMessage\(err, '批量操作失败'\)\)/);
});

test('Codex KPI switches between failover scope and active-channel scope', () => {
  const failoverBlock = source.match(/const failoverOverviewCards = \[([\s\S]*?)\n  \];\n  const activeChannelOverviewCards/)?.[1] || '';
  const failoverTitles = ['参与端点', '健康端点', '跳过检测', '异常端点', '未参与端点', '参与渠道'];
  let previous = -1;
  for (const title of failoverTitles) {
    const index = failoverBlock.indexOf(`title: '${title}'`);
    assert.ok(index > previous, `${title} should follow the previous failover KPI card`);
    previous = index;
  }

  const activeBlock = source.match(/const activeChannelOverviewCards = \[([\s\S]*?)\n  \];\n  const overviewCards/)?.[1] || '';
  const activeTitles = ['凭证分组', '参与端点', '健康端点', '跳过检测', '异常端点', '总端点'];
  previous = -1;
  for (const title of activeTitles) {
    const index = activeBlock.indexOf(`title: '${title}'`);
    assert.ok(index > previous, `${title} should follow the previous active-channel KPI card`);
    previous = index;
  }
  assert.match(source, /const overviewCards = channelFailoverEnabled \? failoverOverviewCards : activeChannelOverviewCards/);
  assert.match(source, /xl:grid-cols-6/);
  assert.match(source, /channelFailoverEnabled \? channel\.failoverEnabled !== false : channel\.isActive/);
});

test('Codex paused-channel endpoints display pause instead of inactive takeover state', () => {
  assert.match(source, /channelPaused = false/);
  assert.match(source, /const showChannelPaused = channelPaused && failoverEnabled/);
  assert.match(source, /channelPaused=\{channelFailoverEnabled && channel\.failoverEnabled === false\}/);
  assert.match(source, /aria-label="渠道已暂停：不参与健康检查与故障转移"/);
  assert.match(source, />\s*暂停\s*<\/span>/);
});

test('Codex active-channel pause uses candidate preflight and confirmation', () => {
  assert.match(source, /getNextAvailableCodexChannel\(channel\.id\)/);
  assert.match(source, /确认暂停活跃渠道/);
  assert.match(source, /暂停后将自动切换到/);
  assert.match(source, /handleConfirmActiveChannelPause/);
  assert.match(source, /channelActivationDisabled = channelFailoverEnabled && channel\.failoverEnabled === false/);
  assert.match(wailsApiSource, /WailsApp\.GetNextAvailableCodexChannel/);
});
