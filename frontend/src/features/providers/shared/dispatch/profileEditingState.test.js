import test from 'node:test';
import assert from 'node:assert/strict';
import { editableProfile, newRoutingProfile, profileChannelIds, validateProfileDraft } from './dispatchState.js';
import { buildRoutingProfilePayload, mapRoutingProfile } from '../../../../services/wails/dispatch.js';
import { copyProfileRules, moveListItem, positionProfileChannel, positionProfileRule, removeProfileRule, ruleCopyIssue, setProfileRulePriority } from './profileEditingState.js';

const profile = (provider, id, extra = {}) => ({ ...newRoutingProfile(provider, false, { maxAttempts: 10, maxDurationMs: 60000 }), id, name: `方案 ${id}`, ...extra });
const target = (channelId = 1, groupId = 0) => ({ channelId, groupId, profileId: 0, upstreamModel: '' });
const rule = (id, extra = {}) => ({ id, rowKey: `rule-${id}`, enabled: true, kind: 'model', name: '', priority: id, matchModel: 'gpt-*', requireStream: null, intentId: '', mode: 'prefer', fallbackToDefault: null, targets: [target()], ...extra });
const resources = (provider, profiles = []) => ({ profiles: profiles.map(item => ({ provider, ...item })), channels: [{ id: 1 }, { id: 2 }, { id: 3 }], accounts: [{ channelId: 1, groupId: 11 }, { channelId: 1, groupId: 12 }, { channelId: 2, groupId: 21 }] });
const keyFactory = () => { let index = 0; return () => `new-key-${++index}`; };

test('position moves support both directions, retain object identity, and reject invalid positions', () => {
  const items = [{ id: 1 }, { id: 2 }, { id: 3 }];
  assert.deepEqual(moveListItem(items, 0, 2).map(item => item.id), [2, 3, 1]);
  assert.deepEqual(moveListItem(items, 2, 0).map(item => item.id), [3, 1, 2]);
  assert.equal(moveListItem(items, 0, 2)[2], items[0]);
  for (const [from, to] of [[-1, 1], [0, 3], [0, 1.5], [2, 2], [NaN, 1]]) assert.equal(moveListItem(items, from, to), items);
  assert.deepEqual(items.map(item => item.id), [1, 2, 3]);
});

test('channel moves carry every credential group, enabled state and weight through the save boundary', () => {
  for (const provider of ['claude', 'codex']) {
    const original = profile(provider, 1, { members: [
      { channelId: 1, groupId: 11, enabled: true, weight: '4' },
      { channelId: 1, groupId: 12, enabled: false, weight: '2' },
      { channelId: 2, groupId: 0, enabled: true, weight: 1 },
      { channelId: 3, groupId: 0, enabled: false, weight: 1 },
    ] });
    const moved = positionProfileChannel(original, 3, 0);
    assert.deepEqual(profileChannelIds(moved), [3, 1, 2]);
    assert.deepEqual(moved.members.map(member => member.position), [0, 1, 2, 3]);
    assert.equal(moved.members[2].enabled, false);
    assert.equal(moved.members[1].weight, '4');
    assert.deepEqual(buildRoutingProfilePayload(moved).members.map(member => [member.channel_id, member.group_id, member.position]), [[3, 0, 0], [1, 11, 1], [1, 12, 2], [2, 0, 3]]);
    assert.equal(positionProfileChannel(original, 9, 0), original);
    assert.equal(positionProfileChannel(original, 1, 0), original);
    assert.deepEqual(profileChannelIds(original), [1, 2, 3]);
  }
});

test('rule drag order persists with equal old priorities; manual priority uses the store tie breaker', () => {
  for (const provider of ['claude', 'codex']) {
    const original = profile(provider, 1, { rules: [rule(1, { priority: 8 }), rule(2, { priority: 8 }), rule(3, { priority: 90 })] });
    const moved = positionProfileRule(original, 'rule-3', 0);
    assert.deepEqual(moved.rules.map(item => [item.id, item.priority]), [[3, 0], [1, 1], [2, 2]]);
    assert.deepEqual(mapRoutingProfile(buildRoutingProfilePayload(moved)).rules.map(item => [item.id, item.priority]), [[3, 0], [1, 1], [2, 2]]);
    const edited = setProfileRulePriority(original, 'rule-3', -2);
    assert.deepEqual(edited.rules.map(item => item.id), [3, 1, 2]);
    assert.deepEqual(setProfileRulePriority(edited, 'rule-3', 8).rules.map(item => item.id), [1, 2, 3]);
    assert.equal(setProfileRulePriority(original, 'rule-1', 1.5), original);
    assert.equal(positionProfileRule(original, 'missing', 0), original);
    assert.equal(original.rules[2].priority, 90);
  }
});

test('deleting a rule closes priority gaps without changing the remaining rule identities or targets', () => {
  for (const provider of ['claude', 'codex']) {
    const original = profile(provider, 1, { rules: [rule(1, { priority: 0, kind: 'non_stream' }), rule(2, { priority: 1 }), rule(3, { priority: 2, enabled: false })] });
    const snapshot = structuredClone(original);
    for (const deleted of original.rules) {
      const remaining = removeProfileRule(original, deleted.rowKey);
      assert.deepEqual(remaining.rules.map(item => item.priority), [0, 1]);
      const expected = original.rules.filter(item => item.id !== deleted.id);
      assert.deepEqual(remaining.rules.map(item => item.id), expected.map(item => item.id));
      assert.deepEqual(remaining.rules.map(item => item.rowKey), expected.map(item => item.rowKey));
      assert.deepEqual(remaining.rules.map(item => item.targets), expected.map(item => item.targets));
      assert.deepEqual(remaining.rules.map(item => item.enabled), expected.map(item => item.enabled));
      assert.deepEqual(mapRoutingProfile(buildRoutingProfilePayload(remaining)).rules.map(item => item.priority), [0, 1]);
    }
    const removedMiddle = removeProfileRule(original, 'rule-2');
    assert.deepEqual(removedMiddle.rules.map(item => [item.id, item.priority]), [[1, 0], [3, 1]]);
    const source = profile(provider, 2, { rules: [rule(9)] });
    const copied = copyProfileRules(removedMiddle, source, [9], resources(provider), keyFactory());
    assert.deepEqual(copied.rules.map(item => item.priority), [0, 1, 2]);
    let empty = original;
    for (const item of original.rules) empty = removeProfileRule(empty, item.rowKey);
    assert.deepEqual(empty.rules, []);
    assert.equal(removeProfileRule(original, 'missing'), original);
    assert.deepEqual(original, snapshot);
  }
});

test('selected rule copies are appended independently with fresh IDs, keys, and original routing semantics', () => {
  for (const provider of ['claude', 'codex']) {
    const createKey = keyFactory();
    const destination = editableProfile(profile(provider, 2, { revision: 7, rules: [rule(7, { priority: 25 })] }), createKey);
    const source = profile(provider, 3, { rules: [rule(7, { priority: 1, requireStream: false, mode: 'fallback', fallbackToDefault: false, targets: [target(2, 21), target(1, 11)] }), rule(8)] });
    const snapshot = structuredClone(source);
    const copied = copyProfileRules(destination, source, [7], resources(provider), createKey);
    assert.equal(copied.id, 2);
    assert.equal(copied.revision, 7);
    assert.equal(copied.rules[0], destination.rules[0]);
    const next = copied.rules[1];
    assert.equal(next.id, 8);
    assert.equal(next.profileId, 2);
    assert.equal(next.priority, 26);
    assert.equal(next.requireStream, false);
    assert.equal(next.mode, 'fallback');
    assert.equal(next.fallbackToDefault, false);
    assert.deepEqual(next.targets.map(item => [item.channelId, item.groupId]), [[2, 21], [1, 11]]);
    assert.equal(new Set([next.rowKey, ...next.targets.map(item => item.rowKey)]).size, 3);
    assert.notEqual(next.rowKey, destination.rules[0].rowKey);
    next.targets[0].upstreamModel = 'changed';
    assert.deepEqual(source, snapshot);
    assert.equal(destination.rules.length, 1);
    assert.equal(validateProfileDraft(copied), '');
    const twice = copyProfileRules(copied, source, [7], resources(provider), createKey);
    assert.equal(twice.rules[2].id, 9);
  }
});

test('intent copies include the fallback closure and remap collisions without changing the classifier', () => {
  for (const provider of ['claude', 'codex']) {
    const classifier = { enabled: false, provider, channelId: 2, groupId: 21, model: 'current', timeoutMs: 8000, confidence: 0.6, failureMode: 'default' };
    const intent = (id, fallbackIntent = '') => ({ id, name: id, description: 'description', enabled: true, priority: 0, targets: [target()], mode: 'prefer', fallbackIntent, fallbackToDefault: null });
    const destination = profile(provider, 2, { classifier, intents: [intent('coding'), intent('coding_copy')] });
    const source = profile(provider, 3, { classifier: { enabled: true }, intents: [intent('coding', 'general'), intent('general', 'coding'), intent('unrelated')], rules: [rule(1, { kind: 'intent', intentId: 'coding' }), rule(2, { kind: 'composite', intentId: 'general' })] });
    const copied = copyProfileRules(destination, source, [1, 2], resources(provider), keyFactory());
    assert.deepEqual(copied.intents.map(item => item.id), ['coding', 'coding_copy', 'coding_copy_2', 'general']);
    assert.deepEqual(copied.rules.map(item => item.intentId), ['coding_copy_2', 'general']);
    assert.equal(copied.intents[2].fallbackIntent, 'general');
    assert.equal(copied.intents[3].fallbackIntent, 'coding_copy_2');
    assert.equal(copied.classifier, classifier);
    assert.equal(copied.classifier.enabled, false);
    assert.equal(validateProfileDraft(copied), '');
  }
});

test('mapping copies respect the destination participating groups including group zero', () => {
  for (const provider of ['claude', 'codex']) {
    const mapping = rule(1, { kind: 'mapping', targets: [{ ...target(1), upstreamModel: 'actual' }] });
    const source = profile(provider, 3, { members: [{ channelId: 1, groupId: 11, enabled: true }], rules: [mapping] });
    const destination = profile(provider, 2);
    assert.match(ruleCopyIssue(destination, source, mapping, resources(provider)), /先.*参与/);
    assert.throws(() => copyProfileRules(destination, source, [1], resources(provider)), /先.*参与/);
    destination.members = [{ channelId: 1, groupId: 11, enabled: true }];
    const copied = copyProfileRules(destination, source, [1], resources(provider), keyFactory());
    assert.equal(copied.rules[0].targets[0].groupId, 0);
    assert.equal(validateProfileDraft(copied), '');
    assert.equal(copied.members, destination.members);
    assert.match(ruleCopyIssue(destination, source, { ...mapping, targets: [{ ...target(1, 12), upstreamModel: 'actual' }] }, resources(provider)), /先.*参与/);
  }
});

test('copy rejects missing resources, missing intent definitions, cross-provider and empty selections', () => {
  const destination = profile('codex', 2);
  const source = profile('codex', 3, { rules: [rule(1)] });
  assert.throws(() => copyProfileRules(destination, source, [], resources('codex')), /请选择/);
  assert.throws(() => copyProfileRules(destination, { ...source, provider: 'claude' }, [1], resources('codex')), /同一服务/);
  assert.match(ruleCopyIssue(destination, source, rule(1, { targets: [target(99)] }), resources('codex')), /不存在/);
  assert.match(ruleCopyIssue(destination, source, rule(1, { targets: [target(1, 99)] }), resources('codex')), /不存在/);
  assert.match(ruleCopyIssue(destination, source, rule(1, { targets: [{ profileId: 99 }] }), resources('codex')), /方案.*不存在/);
  assert.match(ruleCopyIssue(destination, source, rule(1, { kind: 'composite', intentId: 'missing' }), resources('codex')), /缺少意图/);
});
