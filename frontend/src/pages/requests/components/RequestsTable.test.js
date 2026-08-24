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
