import test from 'node:test';
import assert from 'node:assert/strict';

const source = await import('node:fs').then(({ readFileSync }) => readFileSync(new URL('./index.jsx', import.meta.url), 'utf8'));
const endpointBadgeSource = await import('node:fs').then(({ readFileSync }) => readFileSync(new URL('../../management/shared/endpointBadges.jsx', import.meta.url), 'utf8'));

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

test('credential group bulk controls and endpoint rows use repeat and shield action icons', () => {
  assert.match(source, /onSetGroupEndpointSkipHealthCheck/);
  assert.match(source, /handleSetGroupEndpointSkipHealthCheck/);
  assert.match(source, /groupSkippedHealthEndpoints/);
  assert.match(source, /groupCheckedHealthEndpoints/);
  assert.match(source, /当前分组全部跳过检测/);
  assert.match(source, /当前分组全部恢复检测/);
  assert.match(source, /const RepeatOffIcon/);
  assert.match(source, /failoverEnabled\s*\?\s*<RepeatOffIcon size=\{12\} \/>\s*:\s*<Repeat size=\{12\} \/>/);
  assert.match(source, /skipHealthCheck\s*\?\s*<ShieldCheck size=\{12\} \/>\s*:\s*<ShieldOff size=\{12\} \/>/);
  assert.match(source, /skipHealthCheck\s*\?\s*'text-slate-400 hover:bg-slate-100 hover:text-purple-600'\s*:\s*'bg-purple-50 text-purple-600 hover:bg-purple-100'/);
  assert.match(source, /shouldSkipGroupHealthCheck\s*\?\s*<ShieldOff size=\{12\} \/>\s*:\s*<ShieldCheck size=\{12\} \/>/);
});

test('Codex failover action hover targets keep a fixed square size', () => {
  const squareFailoverButtons = source.match(/className=\{`inline-flex size-5 flex-shrink-0 items-center justify-center rounded-md/g) || [];
  assert.equal(squareFailoverButtons.length, 2);
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
