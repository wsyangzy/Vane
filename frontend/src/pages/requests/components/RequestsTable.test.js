import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';

const source = fs.readFileSync(new URL('./RequestsTable.jsx', import.meta.url), 'utf8');
const detailSource = fs.readFileSync(new URL('./RequestDetailModal.jsx', import.meta.url), 'utf8');
const pageSource = fs.readFileSync(new URL('../index.jsx', import.meta.url), 'utf8');

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
