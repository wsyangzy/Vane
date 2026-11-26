import test from 'node:test';
import assert from 'node:assert/strict';
import { credentialGroupRoutingImpact, profileSwitchBlockReason, ruleDraftIssue, ruleTargetLabels } from './dispatchState.js';
import { addProfileChannel, candidateHealthPresentation, dispatchReason, editableProfile, mappingTargetAllowed, moveProfileChannel, newRoutingProfile as createRoutingProfile, quotaPresentation, selectProfileChannels, stateCompatibilityLabel, updateProfileMembers, validateProfileDraft } from './dispatchState.js';

const newRoutingProfile = (provider, isDefault) => createRoutingProfile(provider, isDefault, { maxAttempts: 10, maxDurationMs: 60000, waitForRecovery: true });

test('profile switching requires an enabled profile with a participating group for both providers', () => {
  for (const provider of ['claude', 'codex']) {
    const profile = { ...newRoutingProfile(provider), name: '待配置方案' };
    assert.equal(profileSwitchBlockReason(profile), '无参与分组');
    assert.equal(validateProfileDraft(profile), '', 'an incomplete non-default profile can still be saved');
    assert.match(validateProfileDraft({ ...profile, isDefault: true }), /至少需要一个参与分组/);
    profile.members = [{ channelId: 1, groupId: 11, enabled: false }];
    assert.equal(profileSwitchBlockReason(profile), '无参与分组');
    profile.members.push({ channelId: 2, groupId: 22, enabled: true });
    assert.equal(profileSwitchBlockReason(profile), '');
    assert.equal(validateProfileDraft({ ...profile, isDefault: true }), '');
    profile.enabled = false;
    assert.equal(profileSwitchBlockReason(profile), '方案已停用');
    assert.match(validateProfileDraft({ ...profile, isDefault: true }), /必须保持可用/);
  }
});

test('dispatch reasons distinguish a reached daily limit from unavailable cost data', () => {
  assert.equal(dispatchReason('daily_cost_limit_exceeded'), '凭证分组已达每日成本限额');
  assert.equal(dispatchReason('daily_cost_state_unavailable'), '暂时无法读取每日成本，未发送请求');
});

test('collapsed rule targets identify channels, scoped groups, profiles and model rewrites', () => {
  const resources = {
    channels: [{ id: 1, name: 'ZeroCat' }, { id: 2, name: '备用' }],
    accounts: [{ channelId: 2, groupId: 11, groupName: '其他渠道分组' }, { channelId: 1, groupId: 11, groupName: '特惠' }],
    profiles: [{ id: 3, name: '常用' }]
  };
  assert.deepEqual(ruleTargetLabels([
    { channelId: 1, groupId: 11, upstreamModel: ' luna-v2 ' },
    { channelId: 2, groupId: 0 },
    { profileId: 3 },
    { targetKind: 'profile', profileId: 0 },
    { channelId: 0 },
    { channelId: 9, groupId: 99 },
  ], resources), ['ZeroCat / 特惠 → luna-v2', '备用 / 全部分组', '方案：常用', '待选择方案', '待选择渠道', '渠道 #9 / 分组 #99']);
});

test('new rules require usable conditions and every target before another rule is added', () => {
  for (const provider of ['claude', 'codex']) {
    const profile = { ...newRoutingProfile(provider), members: [{ channelId: 1, groupId: 11, enabled: true }] };
    const rule = { kind: 'model', matchModel: '  ', targets: [] };
    assert.match(ruleDraftIssue(profile, rule), /请求模型/);
    rule.matchModel = 'gpt-*';
    assert.match(ruleDraftIssue(profile, rule), /至少一个/);
    rule.targets.push({ channelId: 0, profileId: 0 });
    assert.match(ruleDraftIssue(profile, rule), /渠道或方案/);
    rule.targets[0].channelId = 1;
    assert.equal(ruleDraftIssue(profile, rule), '');
    rule.targets.push({ targetKind: 'profile', profileId: 0, channelId: 1 });
    assert.match(ruleDraftIssue(profile, rule), /渠道或方案/);
    rule.targets[1].profileId = 2;
    assert.equal(ruleDraftIssue(profile, rule), '');
    assert.match(ruleDraftIssue(profile, { kind: 'non_stream', targets: [] }), /至少一个/);
    assert.equal(ruleDraftIssue(profile, { kind: 'non_stream', targets: [{ profileId: 2 }] }), '');
    const mapping = { kind: 'mapping', matchModel: 'client-model', targets: [{ channelId: 1, groupId: 11, upstreamModel: ' ' }] };
    assert.match(ruleDraftIssue(profile, mapping), /实际模型/);
    mapping.targets[0].upstreamModel = 'actual-model';
    assert.equal(ruleDraftIssue(profile, mapping), '');
    mapping.targets[0].groupId = 12;
    assert.match(ruleDraftIssue(profile, mapping), /已参与/);
  }
});

test('regexp model rules retain their expression through editing and reject an empty expression', () => {
  for (const provider of ['claude', 'codex']) {
    const profile = { ...newRoutingProfile(provider), name: '正则路由', rules: [{ kind: 'model', enabled: true, matchModel: String.raw`regex:gpt-\D+`, targets: [{ channelId: 2 }] }] };
    const edited = editableProfile(profile, () => 'stable-key');
    assert.equal(edited.rules[0].matchModel, String.raw`regex:gpt-\D+`);
    assert.equal(ruleDraftIssue(edited, edited.rules[0]), '');
    assert.equal(validateProfileDraft(edited), '');
    edited.rules[0].matchModel = ' ReGeX:  ';
    assert.match(ruleDraftIssue(edited, edited.rules[0]), /正则表达式/);
    assert.match(validateProfileDraft(edited), /正则表达式/);
  }
});

test('new profiles use saved global budgets and existing profiles retain explicit limits', () => {
  const saved = { maxAttempts: 24, maxDurationMs: 150000, waitForRecovery: false };
  const profile = createRoutingProfile('codex', false, saved);
  profile.name = '使用全局初始值';
  assert.equal(profile.maxAttempts, 24);
  assert.equal(profile.maxDurationMs, 150000);
  assert.equal(profile.waitForRecovery, false);
  assert.equal(validateProfileDraft(profile), '');
  const existing = editableProfile({ ...profile, maxAttempts: 3, maxDurationMs: 10000 });
  assert.equal(existing.maxAttempts, 3);
  assert.equal(existing.maxDurationMs, 10000);
  assert.match(validateProfileDraft({ ...createRoutingProfile('codex'), name: '未读取设置' }), /尝试次数/);
});

test('health presentation distinguishes disabled resources, credential repair and timed recovery', () => {
  const disabled = candidateHealthPresentation({ enabled: false, health: { coolingDown: true, reason: 'account_disabled', until: '2026-10-23T10:00:00Z' } });
  assert.equal(disabled.label, '分组已停用'); assert.equal(disabled.until, null); assert.equal(disabled.tone, 'muted');
  assert.equal(candidateHealthPresentation({ enabled: false, health: {} }).label, '待配置');
  const repair = candidateHealthPresentation({ enabled: true, health: { repairRequired: true, coolingDown: true } });
  assert.equal(repair.label, '凭证待修复'); assert.equal(repair.until, null);
  const cooling = candidateHealthPresentation({ enabled: true, health: { coolingDown: true, reason: 'account_cooling_down', until: '2026-10-23T10:00:00Z' } });
  assert.equal(cooling.label, '冷却中'); assert.equal(cooling.until, '2026-10-23T10:00:00Z');
  assert.equal(candidateHealthPresentation({ enabled: true, health: {} }).label, '正常');
  assert.match(dispatchReason('state_origin_unknown'), /新建会话/);
});

test('profile membership ignores legacy switches and preserves its own selections when reordered', () => {
  const initial = newRoutingProfile('codex');
  const accounts = [{ channelId: 1, groupId: 11, enabled: true }, { channelId: 1, groupId: 12, enabled: false }, { channelId: 2, groupId: 21, enabled: true }];
  const added = addProfileChannel(addProfileChannel(initial, 1, accounts), 2, accounts);
  assert.ok(added.members.every((member) => member.enabled));
  added.members[1].enabled = false;
  const moved = moveProfileChannel(added, 2, -1);
  assert.deepEqual(moved.members.map(({ channelId, groupId, enabled, position }) => [channelId, groupId, enabled, position]), [[2, 21, true, 0], [1, 11, true, 1], [1, 12, false, 2]]);
  assert.equal(initial.members.length, 0);
  assert.equal(addProfileChannel(added, 1, accounts), added);
});

test('confirmed channel selection removes unchecked channels and preserves retained group settings and order', () => {
  const profile = { ...newRoutingProfile('claude'), members: [
    { channelId: 2, groupId: 21, enabled: false, weight: '7', upstreamModel: 'custom-model', position: 0 },
    { channelId: 1, groupId: 11, enabled: true, weight: 2, upstreamModel: '', position: 1 },
    { channelId: 2, groupId: 22, enabled: true, weight: 3, upstreamModel: '', position: 2 },
  ] };
  const before = structuredClone(profile);
  const next = selectProfileChannels(profile, [3, 2, 3], [{ channelId: 3, groupId: 31, enabled: false }]);
  assert.deepEqual(next.members.map(({ channelId, groupId, position }) => [channelId, groupId, position]), [[2, 21, 0], [2, 22, 1], [3, 31, 2]]);
  assert.equal(next.members[0].enabled, false);
  assert.equal(next.members[0].weight, '7');
  assert.equal(next.members[0].upstreamModel, 'custom-model');
  assert.equal(next.members[2].enabled, true);
  assert.deepEqual(profile, before);
  assert.deepEqual(selectProfileChannels(profile, [], []).members, []);
});

test('editable rule and target keys remain independent of editable identity', () => {
  const value = newRoutingProfile('claude');
  value.rules = [{ id: 1, name: 'coding', targets: [{ channelId: 1 }] }];
  let key = 0;
  const draft = editableProfile(value, () => `row-${++key}`);
  draft.rules[0].name = 'analysis';
  draft.rules[0].targets[0].channelId = 2;
  assert.equal(draft.rules[0].rowKey, 'row-1');
  assert.equal(draft.rules[0].targets[0].rowKey, 'row-2');
  assert.equal(value.rules[0].name, 'coding');
});

test('group selection preserves model, weight and other channels while adding new groups in channel order', () => {
  const profile = { ...newRoutingProfile('claude'), members: [
    { channelId: 1, groupId: 11, enabled: true, weight: '7', upstreamModel: 'custom' },
    { channelId: 2, groupId: 21, enabled: true, weight: 2, upstreamModel: '' },
  ] };
  const off = updateProfileMembers(profile, 1, [11, 12], { enabled: false });
  assert.equal(off.members[0].upstreamModel, 'custom');
  assert.equal(off.members[0].weight, '7');
  assert.deepEqual(off.members.map(({ channelId, groupId, enabled, position }) => [channelId, groupId, enabled, position]), [[1, 11, false, 0], [1, 12, false, 1], [2, 21, true, 2]]);
  const on = updateProfileMembers(off, 1, [11, 12], { enabled: true });
  assert.equal(on.members[0].upstreamModel, 'custom');
  assert.equal(on.members[0].weight, '7');
  assert.equal(on.members[1].weight, 1);
  assert.deepEqual(on.members[2], off.members[2]);
  assert.equal(profile.members.length, 2);
  assert.equal(profile.members[0].enabled, true);
});

test('draft validation rejects invalid budgets', () => {
  const profile = newRoutingProfile('codex'); profile.name = '默认';
  assert.equal(validateProfileDraft(profile), '');
  profile.maxDurationMs = 'NaN';
  assert.match(validateProfileDraft(profile), /预算/);
});

test('quota presentation preserves unknown and historical quota without claiming fresh values', () => {
  const now = Date.now();
  assert.equal(quotaPresentation({ confidence: 'unknown', usedPercent: null, expiresAt: new Date(now + 1000).toISOString() }, now).label, '未知');
  const stale = quotaPresentation({ confidence: 'exact', usedPercent: 0, expiresAt: new Date(now - 1000).toISOString() }, now);
  assert.equal(stale.known, false); assert.equal(stale.label, '0.0%（历史）');
  assert.equal(quotaPresentation({ confidence: 'exact', usedPercent: 0, expiresAt: new Date(now + 1000).toISOString() }, now).known, true);
});

test('model mapping is limited to selected groups and requires an explicit actual model', () => {
  const profile = { ...newRoutingProfile('codex'), name: '默认', members: [{ channelId: 1, groupId: 11, enabled: true }, { channelId: 1, groupId: 12, enabled: false }] };
  assert.equal(mappingTargetAllowed(profile, { channelId: 1, groupId: 11 }), true);
  for (const target of [{ channelId: 2, groupId: 11 }, { channelId: 1, groupId: 12 }, { profileId: 2, channelId: 1, groupId: 11 }]) {
    assert.equal(mappingTargetAllowed(profile, target), false);
  }
  profile.rules = [{ kind: 'mapping', enabled: true, matchModel: 'client-model', targets: [{ channelId: 1, groupId: 11, upstreamModel: '' }] }];
  assert.match(validateProfileDraft(profile), /实际模型/);
  profile.rules[0].targets[0].upstreamModel = 'actual-model';
  assert.equal(validateProfileDraft(profile), '');
  profile.members[0].enabled = false;
  assert.match(validateProfileDraft(profile), /已参与/);
  profile.rules[0].enabled = false;
  assert.equal(validateProfileDraft(profile), '');
});

test('channel-wide model mapping follows participating groups for both providers', async () => {
  const { ruleDraftIssue } = await import('./dispatchState.js');
  for (const provider of ['claude', 'codex']) {
    const profile = { ...newRoutingProfile(provider), name: '全部已参与分组', members: [
      { channelId: 1, groupId: 11, enabled: true },
      { channelId: 1, groupId: 12, enabled: false },
    ] };
    const target = { channelId: 1, groupId: 0, upstreamModel: 'actual-model' };
    const rule = { kind: 'mapping', enabled: true, matchModel: 'client-model', targets: [target] };
    profile.rules = [rule];
    assert.equal(mappingTargetAllowed(profile, target), true);
    assert.equal(ruleDraftIssue(profile, rule), '');
    assert.equal(validateProfileDraft(profile), '');
    profile.members[0].enabled = false;
    assert.equal(mappingTargetAllowed(profile, target), false);
    assert.match(ruleDraftIssue(profile, rule), /已参与/);
    assert.match(validateProfileDraft(profile), /已参与/);
    profile.members = [{ channelId: 1, groupId: 0, enabled: true }];
    assert.equal(mappingTargetAllowed(profile, target), true);
    assert.equal(mappingTargetAllowed(profile, { ...target, groupId: 12 }), true);
  }
});

test('state compatibility labels distinguish known and unknown states', () => {
  assert.equal(stateCompatibilityLabel('incompatible'), '状态不兼容');
  assert.equal(stateCompatibilityLabel(undefined), '状态兼容性未知');
});

test('credential deletion impact includes members, rules, wildcard channels and provider isolation', () => {
  const profile = (id, name, patch = {}) => ({ id, name, members: [], rules: [], ...patch });
  const workspaces = [
    { provider: 'claude', accounts: [{ groupId: 44, channelId: 7 }], profiles: [
      profile(1, 'member', { members: [{ profileId: 1, groupId: 44, channelId: 7, enabled: true }] }),
      profile(2, 'disabled reference', { members: [{ profileId: 2, groupId: 44, channelId: 7, enabled: false }] }),
      profile(3, 'model rule', { rules: [{ targets: [{ groupId: 44, channelId: 7 }] }] }),
      profile(5, 'wildcard', { members: [{ profileId: 5, groupId: 0, channelId: 7, enabled: true }] }),
      profile(6, 'other channel', { members: [{ groupId: 0, channelId: 8, enabled: true }] }),
      profile(7, 'profile expansion', { rules: [{ targets: [{ profileId: 2, groupId: 44 }] }] }),
    ] },
    { provider: 'codex', accounts: [{ groupId: 44, channelId: 7 }], profiles: [
      profile(8, 'same ID in other provider', { members: [{ groupId: 44, channelId: 7, enabled: true }] }),
    ] },
  ];
  const impact = credentialGroupRoutingImpact(workspaces, 'claude', 44);
  assert.deepEqual(impact.map((item) => item.id), [1, 3, 5]);
  assert.equal(impact[0].losesLastMember, true);
  assert.equal(impact[1].losesLastMember, false);
  assert.deepEqual(credentialGroupRoutingImpact(workspaces, 'codex', 44).map((item) => item.id), [8]);
  assert.deepEqual(credentialGroupRoutingImpact(workspaces, 'claude', 0), []);
});
