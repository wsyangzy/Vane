import assert from 'node:assert/strict';
import test from 'node:test';
import { buildRequestFilterResources, requestFilterPath } from './filterResources.js';

const claudeChannels = [
  { name: '渠道 A', credentialGroups: [{ id: 11, name: '默认分组', apiKey: 'secret' }, { id: 12, name: '备用分组' }, { id: 13, name: '暂无端点' }] },
  { name: '渠道 B', credentialGroups: [{ id: 21, name: '默认分组' }] }
];
const codexChannels = [{
  name: '渠道 A', failoverEnabled: false,
  credentialGroups: [{ id: 31, name: '默认分组', enabled: false }, { id: 32, name: 'Codex 分组' }],
  endpoints: [{ name: 'Codex 端点', credentialGroupId: 31 }, { name: 'Codex 备用', credentialGroupName: 'Codex 分组' }]
}];
const resources = buildRequestFilterResources(claudeChannels, codexChannels);

test('request filter tree keeps identical group and channel names under their own provider', () => {
  const claude = resources.channels.find(provider => provider.provider === 'claude');
  const codex = resources.channels.find(provider => provider.provider === 'codex');
  assert.deepEqual(new Set(claude.channels.find(channel => channel.name === '渠道 A').credentialGroups), new Set(['默认分组', '备用分组', '暂无端点']));
  assert.deepEqual(new Set(codex.channels.find(channel => channel.name === '渠道 A').credentialGroups), new Set(['默认分组', 'Codex 分组']));
  assert.deepEqual(claude.channels.find(channel => channel.name === '渠道 B').credentialGroups, ['默认分组']);
});

test('request filter tree retains resource names without credentials or endpoint metadata', () => {
  assert.ok(!JSON.stringify(resources).includes('secret'));
  assert.ok(!JSON.stringify(resources).includes('Codex 端点'));
  assert.ok(!JSON.stringify(resources).includes('failoverEnabled'));
  assert.equal(resources.channels.find(provider => provider.provider === 'codex').channels.length, 1);
});

test('request filter paths distinguish every selection depth and both providers', () => {
  assert.equal(requestFilterPath(resources.channels, {}), '所有渠道');
  assert.equal(requestFilterPath(resources.channels, { provider: 'claude' }), 'Claude / 全部渠道');
  assert.equal(requestFilterPath(resources.channels, { provider: 'claude', channel: '渠道 A' }), 'Claude / 渠道 A / 全部分组');
  assert.equal(requestFilterPath(resources.channels, { provider: 'claude', channel: '渠道 A', group: '默认分组' }), 'Claude / 渠道 A / 默认分组');
  assert.equal(requestFilterPath(resources.channels, { provider: 'codex', channel: '渠道 A', group: '默认分组' }), 'Codex / 渠道 A / 默认分组');
});

test('request filter tree merges duplicate resource names within the same parent only', () => {
  const tree = buildRequestFilterResources([
    { name: '渠道', credentialGroups: [{ name: '分组' }, { name: '分组' }, { name: '' }] },
    { name: '渠道', credentialGroups: [{ name: '另一分组' }] },
    { name: '' }
  ]);
  assert.equal(tree.channels[0].channels.length, 1);
  assert.deepEqual(new Set(tree.channels[0].channels[0].credentialGroups), new Set(['分组', '另一分组']));
});

test('empty providers and channels can still be selected as a whole', () => {
  const tree = buildRequestFilterResources([{ name: '空渠道' }]);
  assert.deepEqual(tree.channels[0].channels[0], { name: '空渠道', credentialGroups: [] });
  assert.deepEqual(tree.channels[1], { provider: 'codex', label: 'Codex', channels: [] });
  assert.equal(requestFilterPath(tree.channels, { provider: 'codex' }), 'Codex / 全部渠道');
});
