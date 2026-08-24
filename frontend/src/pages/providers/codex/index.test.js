import test from 'node:test';
import assert from 'node:assert/strict';
import { buildCodexEndpointMutationPayload, selectPersistedEndpoints } from '../../management/shared/endpointActions.js';
import { formatMultiplierInputValue } from '../../../utils/multiplier.js';

const source = await import('node:fs').then(({ readFileSync }) => readFileSync(new URL('./index.jsx', import.meta.url), 'utf8'));
const endpointBadgeSource = await import('node:fs').then(({ readFileSync }) => readFileSync(new URL('../../management/shared/endpointBadges.jsx', import.meta.url), 'utf8'));
const wailsApiSource = await import('node:fs').then(({ readFileSync }) => readFileSync(new URL('../../../utils/wailsApi.js', import.meta.url), 'utf8'));

test('Codex endpoint mutations preserve endpoint identity and requested state', () => {
  assert.deepEqual(buildCodexEndpointMutationPayload({
    name: 'primary',
    base_url: 'https://codex.test',
    credential_group_id: 7,
    priority: 2,
    timeout_seconds: 90
  }, { failoverEnabled: false }), {
    name: 'primary',
    baseURL: 'https://codex.test',
    credentialGroupId: 7,
    priority: 2,
    timeoutSeconds: 90,
    failoverEnabled: false
  });
});

test('Codex bulk actions target only persisted endpoints', () => {
  assert.deepEqual(selectPersistedEndpoints([{ id: 1 }, { id: null }, { id: 4 }]), [{ id: 1 }, { id: 4 }]);
});

test('Codex multiplier form state preserves fractional expressions', () => {
  assert.equal(formatMultiplierInputValue('40/30'), '40/30');
});

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

test('credential-group service-tier billing configures four independent multiplier sets', () => {
  assert.match(source, /ServiceTierMultiplierSettings/);
  assert.match(source, /disableServiceTierPricing/);
  assert.match(source, /serviceTierMultipliers/);
  assert.match(source, /Standard、Batch、Flex 或 Fast mode/);
  assert.match(source, /handleServiceTierToggle/);
  assert.match(source, /handleServiceTierSave/);
});

test('Codex credential groups preserve single-click switching and use double-click details', () => {
  assert.match(source, /dailyCostLimitUSD/);
  assert.match(source, /每日成本限额（USD）/);
  assert.match(source, /onClick=\{\(\) => setSelectedCredentialGroupID/);
  assert.match(source, /onDoubleClick=\{\(\) => onOpenCredentialGroup/);
  assert.match(source, /单击切换，双击查看详情/);
  assert.match(source, /CredentialGroupDetailModal/);
  assert.match(source, /onDoubleClick=\{\(event\) => event\.stopPropagation\(\)\}/);
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
  assert.match(source, /const priorityValue = String\(formData\.priority \?\? ''\)\.trim\(\)/);
  assert.match(source, /priority:\s*!isEdit && !hasPriority \? -1 : parsedPriority/);
  assert.match(source, /channelFailoverEnabled && \(/);
  assert.match(source, /留空按现有顺序自动分配；填写已有优先级时交换/);
  assert.match(wailsApiSource, /priority:\s*Number\.parseInt\(input\?\.priority, 10\) \|\| 0/);
  assert.doesNotMatch(wailsApiSource, /priority:\s*Math\.max\(0, Number\.parseInt\(input\?\.priority/);
});

test('Codex failover channel form places model before priority in equal columns', () => {
  assert.match(source, /className=\{channelFailoverEnabled \? 'grid grid-cols-1 gap-4 sm:grid-cols-2' : 'grid grid-cols-1 gap-4'\}/);
  const modelIndex = source.indexOf('模型（可选）');
  const priorityIndex = source.indexOf('>优先级</span>');
  assert.ok(modelIndex >= 0 && priorityIndex > modelIndex, 'model should be rendered before priority');
});

test('Codex channel creation keeps credentials out of the channel modal', () => {
  const channelTabs = source.match(/const CODEX_CHANNEL_FORM_TABS = \[([\s\S]*?)\];/)?.[1] || '';
  assert.match(channelTabs, /基础信息/);
  assert.match(channelTabs, /模型映射/);
  assert.doesNotMatch(channelTabs, /凭证分组/);
  assert.doesNotMatch(source, /activeTab === 'credential'/);
  assert.match(source, /apiKey: ''/);
});

test('new Codex channels continue through credential group and endpoint setup', () => {
  assert.match(source, /const createdChannel = refreshedChannels\.find\(\(channel\) => channel\?\.name === payload\.name\)/);
  assert.match(source, /if \(isCreating\) \{[\s\S]*?openCreateCredentialGroupForm\(createdChannel, true\)/);
  assert.match(source, /const \[initialSetupChannelID, setInitialSetupChannelID\] = useState\(null\)/);
  assert.match(source, /const shouldContinueInitialSetup = initialSetupChannelID === targetChannel\.id/);
  assert.match(source, /shouldContinueInitialSetup && savedGroup\?\.id && copyEndpoints\.length === 0/);
  assert.match(source, /openCreateEndpointForm\(targetChannel, savedGroup\)/);
  assert.match(source, /continueToEndpoint=\{initialSetupChannelID === credentialGroupFormChannel\?\.id\}/);
  assert.match(source, /\{isEdit \? '保存修改' : '下一步'\}/);
  assert.match(source, /<Button variant="primary" icon=\{isEdit \? Pencil : undefined\}/);
  assert.match(source, /\{isEdit \? '保存分组' : \(continueToEndpoint \|\| copyEndpointsEnabled \? '下一步' : '添加分组'\)\}/);
});

test('Codex ordinary credential group creation does not enter endpoint setup', () => {
  assert.match(source, /const openCreateCredentialGroupForm = \(channel, isInitialSetup = false\) => \{\s*setInitialSetupChannelID\(isInitialSetup \? channel\?\.id \?\? null : null\)/);
  assert.match(source, /const openEditCredentialGroupForm = \(channel, group\) => \{\s*setInitialSetupChannelID\(null\)/);
  assert.match(source, /const closeCredentialGroupForm = \(\) => \{[\s\S]*?setInitialSetupChannelID\(null\)/);
});

test('Codex credential group and endpoint saves separate mutation errors from refresh errors', () => {
  const groupStart = source.indexOf('const handleSaveCredentialGroup = async');
  const groupEnd = source.indexOf('const handleDeleteCredentialGroup', groupStart);
  const groupHandler = source.slice(groupStart, groupEnd);
  assert.ok(groupHandler.indexOf('closeCredentialGroupForm();') < groupHandler.indexOf('await fetchData({ silent: true, throwOnError: true });'));
  assert.match(groupHandler, /setCredentialGroupFormError\(getErrorMessage\(err, '保存凭证分组失败'\)\);\s*return;/);
  assert.match(groupHandler, /setError\(`凭证分组已保存，但列表刷新失败/);

  const endpointStart = source.indexOf('const handleSaveEndpoint = async');
  const endpointEnd = source.indexOf('const handleActivateEndpoint', endpointStart);
  const endpointHandler = source.slice(endpointStart, endpointEnd);
  assert.equal((endpointHandler.match(/await fetchData\(\{ silent: true, throwOnError: true \}\);/g) || []).length, 2);
  assert.equal((endpointHandler.match(/setError\(`端点已保存，但列表刷新失败/g) || []).length, 2);
  assert.match(endpointHandler, /setEndpointFormError\(getErrorMessage\(err, '更新端点失败'\)\);\s*return;/);
  assert.match(endpointHandler, /setEndpointFormError\(getErrorMessage\(err, '保存端点失败'\)\);\s*return;/);
});

test('Codex creation flow does not silently stop when channel refresh fails', () => {
  assert.match(source, /let channelCreated = false/);
  assert.match(source, /channelCreated = true/);
  assert.match(source, /fetchData\(\{ silent: true, throwOnError: true \}\)/);
  assert.match(source, /if \(!createdChannel\) \{\s*throw new Error\('渠道已创建，但刷新后未找到该渠道/);
  assert.match(source, /if \(throwOnError\) throw err/);
  assert.match(source, /if \(channelCreated\) \{[\s\S]*?closeChannelForm\(\)[\s\S]*?setError\(/);
});

test('Codex channels without credential groups do not expose endpoint actions', () => {
  assert.match(source, /<div hidden=\{!selectedCredentialHasCredential\} className="flex flex-shrink-0 items-center gap-1">[\s\S]*?添加端点[\s\S]*?<\/div>/);
  assert.match(source, /const selectedCredentialHasCredential = Boolean\(selectedCredentialGroup\) &&[\s\S]*?hasCodexCredential\(selectedCredentialGroup, channel\)/);
  assert.match(source, /const targetCredentialGroup = credentialGroup \|\| getDefaultCredentialGroup\(channel\);[\s\S]*?if \(!channel \|\| !targetCredentialGroup \|\| !hasCodexCredential\(targetCredentialGroup, channel\)\) return;/);
  assert.match(source, /请先添加凭证分组并配置 API Key，再添加端点。/);
});

test('Codex credential gating covers mapped and legacy API key fields', () => {
  assert.match(source, /const hasCodexCredential = \(\.\.\.sources\)/);
  assert.match(source, /source\?\.apiKey/);
  assert.match(source, /source\?\.api_key/);
  assert.match(source, /source\?\.apiKeyMasked/);
  assert.match(source, /source\?\.api_key_masked/);
  assert.match(source, /hasCodexCredential\(channel\)/);
  assert.match(source, /const selectedCredentialHasCredential/);
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

test('Codex billing mode uses radio cards instead of view tabs', () => {
  // 计费模式是二选一，必须是单选语义；沿用 SegmentedTabs 会与弹窗顶部的视图标签混淆
  assert.match(source, /const CODEX_BILLING_MODES/);
  assert.doesNotMatch(source, /CODEX_BILLING_MODE_TABS/);
  assert.match(source, /role="radiogroup"/);
  assert.match(source, /role="radio"/);
  assert.match(source, /aria-checked=\{selected\}/);
  assert.match(source, /仅所选模式生效/);
  // 两种模式各自带说明，避免用户不清楚切换的后果
  assert.match(source, /五项成本倍率直接相乘/);
  assert.match(source, /按模型 \/ 档位 \/ 上下文匹配规则/);
});
