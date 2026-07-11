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

test('Claude credential group and endpoint controls match Codex repeat and shield action icons', () => {
  assert.match(source, /const RepeatOffIcon/);
  assert.match(source, /onSetGroupEndpointSkipHealthCheck/);
  assert.match(source, /handleSetGroupEndpointSkipHealthCheck/);
  assert.match(source, /groupSkippedHealthEndpoints/);
  assert.match(source, /groupCheckedHealthEndpoints/);
  assert.match(source, /当前分组全部跳过检测/);
  assert.match(source, /当前分组全部恢复检测/);
  assert.match(source, /failoverEnabled\s*\?\s*<RepeatOffIcon size=\{12\} \/>\s*:\s*<Repeat size=\{12\} \/>/);
  assert.match(source, /isSkipHealthCheck\s*\?\s*<ShieldCheck size=\{12\} \/>\s*:\s*<ShieldOff size=\{12\} \/>/);
  assert.match(source, /isSkipHealthCheck\s*\?\s*'text-slate-400 hover:bg-slate-100 hover:text-purple-600'\s*:\s*'bg-purple-50 text-purple-600 hover:bg-purple-100'/);
  assert.match(source, /shouldSkipGroupHealthCheck\s*\?\s*<ShieldOff size=\{12\} \/>\s*:\s*<ShieldCheck size=\{12\} \/>/);
});

test('Claude failover action hover targets keep a fixed square size', () => {
  const squareFailoverButtons = source.match(/className=\{`inline-flex size-5 flex-shrink-0 items-center justify-center rounded-md/g) || [];
  assert.equal(squareFailoverButtons.length, 2);
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
