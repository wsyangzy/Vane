import test from 'node:test';
import assert from 'node:assert/strict';
import { selectPersistedEndpoints } from '../shared/endpoints/endpointActions.js';
import { formatMultiplierInputValue } from '../../../shared/lib/multiplier.js';
import { buildClaudeEndpointCopyPayload } from './utils/endpointPayload.js';

const source = await import('node:fs').then(({ readFileSync }) => [
  'model.js',
  'components/endpointState.jsx',
  'components/DeleteChannelConfirmDialog.jsx',
  'components/DeleteCredentialGroupConfirmDialog.jsx',
  'components/PauseActiveChannelConfirmDialog.jsx',
  'components/EndpointMiniCard.jsx',
  'components/EndpointDetailModal.jsx',
  'components/CreateChannelModal.jsx',
  'components/ClaudeCredentialGroupFormModal.jsx',
  'components/ChannelCard.jsx',
  'index.jsx',
].map((name) => readFileSync(new URL(name, import.meta.url), 'utf8')).join('\n'));

test('Claude endpoint copy payload preserves channel, group and endpoint settings', () => {
  assert.deepEqual(buildClaudeEndpointCopyPayload({
    name: 'secondary',
    base_url: 'https://example.test',
    failover_enabled: false,
    skip_health_check: true,
    timeout_seconds: 45
  }, 'Claude A', 12), {
    channel: 'Claude A',
    credentialGroupId: 12,
    name: 'secondary',
    url: 'https://example.test',
    headers: {},
    priority: undefined,
    failoverEnabled: false,
    skipHealthCheck: true,
    cooldownSeconds: null,
    timeoutSeconds: 45,
    supportsCountTokens: false
  });
});

test('Claude bulk actions target only persisted endpoints', () => {
  assert.deepEqual(selectPersistedEndpoints([{ id: 3 }, {}, null, { id: 0 }, { id: 9 }]), [{ id: 3 }, { id: 9 }]);
});

test('Claude multiplier form state preserves fractional expressions', () => {
  assert.equal(formatMultiplierInputValue('40/30'), '40/30');
});

test('credential-group billing keeps the standard multiplier section', () => {
  assert.match(source, /普通成本倍率/);
  assert.match(source, /STANDARD_CREDENTIAL_GROUP_MULTIPLIER_FIELDS\.map/);
});

test('credential-group standard multipliers are configured in a modal with pricing reference', () => {
  assert.match(source, /MultiplierSettingsModal/);
  assert.match(source, /setStandardMultiplierSettingsOpen\(true\)/);
  assert.match(source, /handleStandardMultiplierSave/);
  assert.match(source, /pricingProvider="claude"/);
  // 每个分项倍率都要能指到对应单价，弹窗才能给出「基础单价 → 实际单价」对照；
  // 5m 与 1h 缓存创建各有独立倍率与独立单价，不能共用一个 priceKey
  assert.match(source, /priceKey: 'inputPrice'/);
  assert.match(source, /cacheCreationCostMultiplier', label: '5分钟缓存创建倍率', priceKey: 'cacheCreationPrice5m'/);
  assert.match(source, /cacheCreationCostMultiplier1h', label: '1小时缓存创建倍率', priceKey: 'cacheCreationPrice1h'/);
});

test('Claude credential groups preserve single-click switching and use double-click details', () => {
  assert.match(source, /dailyCostLimitUSD/);
  assert.match(source, /每日成本限额（USD）/);
  assert.match(source, /CredentialGroupDetailModal/);
  // 分组选择器已提取为共享堆叠卡片，页面只负责接线；
  // 单击/双击的交互契约由 management/shared/CredentialGroupStrip.test.js 保证
  assert.match(source, /onSelect=\{setSelectedCredentialGroupID\}/);
  assert.match(source, /onOpen=\{\(group\) => onOpenCredentialGroup\?\.\(channelName, group\)\}/);
});

test('Claude credential groups no longer expose long-context pricing', () => {
  // 入口已移除：表单开关、倍率概览、编辑弹窗与详情卡片都不应残留
  assert.doesNotMatch(source, /handleLongContextToggle/);
  assert.doesNotMatch(source, /longContextSettingsOpen/);
  assert.doesNotMatch(source, /longContextPricingEnabled/);
  // 匹配 JSX 中的可见文案而非注释，避免代码注释里提及 Long context 时误报
  assert.doesNotMatch(source, /label="Long context 分组倍率"/);
  assert.doesNotMatch(source, />Long context 计费</);
  // 计费闸门恒关：倍率字段仍原样回传以免抹掉历史值，但禁用标志固定为 true
  assert.match(source, /disableLongContextPricing:\s*true/);
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
  assert.match(source, /const showPriority = isEdit \|\| channelFailoverEnabled/);
  assert.match(source, /isEdit \? String\(initialValue\?\.priority \?\? 1\) : ''/);
  assert.match(source, /min=\{1\}/);
  assert.doesNotMatch(source, /留空按现有顺序自动分配；填写已有优先级时交换/);
  assert.match(source, /channelFailoverEnabled=\{channelFailoverEnabled\}/);
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

test('new Claude channels continue through the default credential group and endpoint setup', () => {
  assert.match(source, /const createdChannel = refreshedChannels\.find\(\(channel\) => channel\?\.name === payload\.name\)/);
  assert.match(source, /const defaultCredentialGroup = getDefaultClaudeCredentialGroup\(createdChannel\)/);
  assert.match(source, /setCredentialGroupFormMode\(defaultCredentialGroup \? 'edit' : 'create'\)/);
  assert.match(source, /const shouldContinueInitialSetup = initialSetupChannelName === credentialGroupFormChannel/);
  assert.match(source, /copyEndpoints\.length === 0/);
  assert.match(source, /openCreateEndpointForm\(targetChannelName, savedGroup\)/);
  assert.match(source, /continueToEndpoint=\{initialSetupChannelName === credentialGroupFormChannel\}/);
  assert.match(source, /\{isEdit \? '保存修改' : '下一步'\}/);
  assert.match(source, /icon=\{isEdit \? CheckCircle2 : undefined\}/);
});

test('Claude credential group save closes or advances before refreshing the page data', () => {
  const start = source.indexOf('const handleSaveCredentialGroup = useCallback');
  const end = source.indexOf('const handleSetDefaultCredentialGroup', start);
  const handler = source.slice(start, end);
  const closeIndex = handler.indexOf('closeCredentialGroupForm();');
  const endpointIndex = handler.indexOf('openCreateEndpointForm(targetChannelName, savedGroup);');
  const refreshIndex = handler.indexOf('await loadStorageStatus({ throwOnError: true });');

  assert.ok(closeIndex >= 0 && endpointIndex > closeIndex && refreshIndex > endpointIndex);
  assert.match(handler, /setCredentialGroupFormError\(getErrorMessage\(err, '保存凭证分组失败'\)\);\s*return;/);
  assert.match(handler, /loadStorageStatus\(\{ throwOnError: true \}\)/);
  assert.match(handler, /loadGroups\(\{ throwOnError: true \}\)/);
  assert.match(handler, /setActionError\(`凭证分组已保存，但列表刷新失败/);
});

test('Claude creation flow does not silently stop when channel refresh fails', () => {
  assert.match(source, /let channelCreated = false/);
  assert.match(source, /channelCreated = true/);
  assert.match(source, /loadChannelsMeta\(\{ throwOnError: true \}\)/);
  assert.match(source, /if \(!createdChannel\) \{\s*throw new Error\('渠道已创建，但刷新后未找到该渠道/);
  assert.match(source, /if \(throwOnError\) throw err/);
  assert.match(source, /if \(channelCreated\) \{[\s\S]*?setShowCreateChannel\(false\)[\s\S]*?setActionError\(/);
});

test('Claude credential groups without a usable secret do not expose endpoint actions', () => {
  assert.match(source, /const hasClaudeCredential = \(group\) => \(isSubscriptionGroup\(group\) && group\.localKeyConfigured\) \|\| \[/);
  assert.match(source, /group\?\.tokenMasked/);
  assert.match(source, /group\?\.token_masked/);
  assert.match(source, /group\?\.apiKeyMasked/);
  assert.match(source, /group\?\.api_key_masked/);
  assert.match(source, /const channelHasCredential = credentialGroups\.some/);
  assert.match(source, /hasEndpoints && channelHasCredential &&/);
  assert.match(source, /showRetryButton && channelHasCredential &&/);
  assert.match(source, /hasGroupInfo && channelHasCredential && !isPaused/);
  assert.match(source, /hasGroupInfo && channelHasCredential && isPaused && !isActive/);
  assert.match(source, /isSqliteMode && selectedCredentialGroupHasCredential &&/);
  assert.match(source, /if \(!name \|\| !hasClaudeCredential\(credentialGroup\)\) return false/);
  assert.match(source, /请先配置凭证分组的 Token 或 API Key，再添加端点。/);
});

test('Claude channel cards keep the paused badge in the credentials header', () => {
  assert.doesNotMatch(source, /按请求模型/);
  assert.doesNotMatch(source, /modelMappingCount/);
  // header 由 CredentialGroupStrip 统一渲染（添加分组按钮已移到该行右侧），
  // 页面只负责把标题和暂停标签传进去
  assert.match(source, /label=\{`Credentials · \$\{credentialGroups\.length\}`\}/);
  assert.match(source, /headerExtra=\{channelFailoverEnabled && isPaused/);
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
