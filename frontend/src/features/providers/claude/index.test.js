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

test('Claude endpoint copying preserves timeout inheritance and explicit overrides', () => {
  for (const seconds of [undefined, 0, 300, 45]) {
    const payload = buildClaudeEndpointCopyPayload({ name: 'copy', timeout_seconds: seconds }, 'Claude A', 12);
    assert.equal(payload.timeoutSeconds, seconds ?? 0);
  }
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

test('new Claude channels keep the first two steps in a draft and commit with the endpoint', () => {
  const firstStep = source.slice(source.indexOf('const handleCreateChannel ='), source.indexOf('const handleOpenEditChannel ='));
  assert.match(firstStep, /setChannelSetupDraft\(\{ channel: payload \}\)/);
  assert.doesNotMatch(firstStep, /await (createChannel|loadChannelsMeta)/);
  assert.match(source, /if \(channelSetupDraft\) \{[\s\S]*?setShowForm\(true\);\s*return;/);
  assert.match(source, /continueToEndpoint=\{Boolean\(channelSetupDraft\)\}/);
  assert.match(source, /await commitChannelSetup\(/);
  assert.match(source, /credentialGroupId: savedGroup\.id/);
  assert.match(source, /remove: \(\) => deleteChannel\(channel\.name, true\)/);
  assert.match(source, /\{isEdit \? '保存修改' : '下一步'\}/);
  assert.match(source, /icon=\{isEdit \? CheckCircle2 : undefined\}/);
});

test('Claude credential group save closes or advances before refreshing the page data', () => {
  const start = source.indexOf('const handleSaveCredentialGroup = useCallback');
  const end = source.indexOf('const handleSetDefaultCredentialGroup', start);
  const handler = source.slice(start, end);
  const closeIndex = handler.indexOf('closeCredentialGroupForm();');
  const refreshIndex = handler.indexOf('await loadStorageStatus({ throwOnError: true });');

  assert.ok(closeIndex >= 0 && refreshIndex > closeIndex);
  assert.match(handler, /setCredentialGroupFormError\(getErrorMessage\(err, '保存凭证分组失败'\)\);\s*return;/);
  assert.match(handler, /loadStorageStatus\(\{ throwOnError: true \}\)/);
  assert.match(handler, /loadGroups\(\{ throwOnError: true \}\)/);
  assert.match(handler, /setActionError\(`凭证分组已保存，但列表刷新失败/);
});

test('Claude setup hides infrequent model policy and distinguishes a saved channel from refresh failure', () => {
  assert.match(source, /filter\(\(tab\) => !continueToEndpoint \|\| tab\.key !== 'model'\)/);
  assert.match(source, /modelPolicy: 'inherit'/);
  assert.match(source, /savedChannelSetup \? '渠道已创建' : '端点已保存'/);
  assert.match(source, /initialSetup=\{Boolean\(channelSetupDraft\)\}/);
  assert.doesNotMatch(source, /initialSetupChannelName/);
});

test('Claude endpoint actions accept subscription bindings without requiring legacy caller keys', () => {
  assert.match(source, /const hasClaudeCredential = \(group\) => \(isSubscriptionGroup\(group\) && Boolean\(group\.subscription\)\) \|\| \[/);
  assert.match(source, /group\?\.tokenMasked/);
  assert.match(source, /group\?\.token_masked/);
  assert.match(source, /group\?\.apiKeyMasked/);
  assert.match(source, /group\?\.api_key_masked/);
  assert.match(source, /if \(!name \|\| !hasClaudeCredential\(credentialGroup\)\) return false/);
});

test('Claude endpoint rows keep one fixed height for every failover state', () => {
  assert.match(source, /className=\{`group\/row relative flex h-\[56px\] min-h-\[56px\] max-h-\[56px\] items-start/);
  assert.doesNotMatch(source, /className=\{`group\/row relative flex min-h-\[56px\] items-start/);
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
