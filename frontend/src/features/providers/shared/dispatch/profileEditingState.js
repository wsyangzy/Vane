import { mappingTargetAllowed, profileChannelIds, ruleDraftIssue } from './dispatchState.js';

export function moveListItem(items, from, to) {
  if (!Number.isInteger(from) || !Number.isInteger(to) || from < 0 || to < 0 || from >= items.length || to >= items.length || from === to) return items;
  const next = [...items];
  const [item] = next.splice(from, 1);
  next.splice(to, 0, item);
  return next;
}

export function positionProfileChannel(profile, channelId, position) {
  const ids = profileChannelIds(profile);
  const next = moveListItem(ids, ids.indexOf(channelId), position);
  if (next === ids) return profile;
  return { ...profile, members: next.flatMap(id => profile.members.filter(member => member.channelId === id)).map((member, index) => ({ ...member, position: index })) };
}

export function positionProfileRule(profile, rowKey, position) {
  const next = moveListItem(profile.rules, profile.rules.findIndex(rule => rule.rowKey === rowKey), position);
  if (next === profile.rules) return profile;
  // Persist the displayed order even when older rules have equal priorities.
  return { ...profile, rules: next.map((rule, priority) => ({ ...rule, priority })) };
}

export function removeProfileRule(profile, rowKey) {
  const remaining = profile.rules.filter(rule => rule.rowKey !== rowKey);
  if (remaining.length === profile.rules.length) return profile;
  return { ...profile, rules: remaining.map((rule, priority) => ({ ...rule, priority })) };
}

export function setProfileRulePriority(profile, rowKey, priority) {
  if (!Number.isSafeInteger(priority)) return profile;
  return { ...profile, rules: profile.rules.map(rule => rule.rowKey === rowKey ? { ...rule, priority } : rule).sort((a, b) => Number(a.priority) - Number(b.priority) || a.id - b.id) };
}

export function ruleCopyIssue(destination, source, rule, resources) {
  if (destination.provider !== source.provider) return '只能复制同一服务的规则';
  if (rule.kind === 'mapping' && rule.enabled && rule.targets.some(target => !mappingTargetAllowed(destination, target))) return '请先在当前方案中参与映射目标的渠道 / 分组';
  const issue = ruleDraftIssue(source, rule);
  if (issue) return `来源规则需先完成：${issue}`;
  for (const target of rule.targets) {
    if (target.profileId) {
      if (!resources.profiles.some(profile => profile.id === target.profileId && profile.provider === source.provider)) return '目标方案已不存在，请先修改来源规则';
    } else if (!resources.channels.some(channel => channel.id === target.channelId) || target.groupId && !resources.accounts.some(account => account.channelId === target.channelId && account.groupId === target.groupId)) return '目标渠道 / 分组已不存在，请先修改来源规则';
  }
  return '';
}

export function copyProfileRules(destination, source, selectedIds, resources, createKey = () => crypto.randomUUID()) {
  if (destination.provider !== source.provider) throw new Error('只能复制同一服务的规则');
  const rules = source.rules.filter(rule => selectedIds.includes(rule.id));
  if (!rules.length) throw new Error('请选择需要复制的规则');
  for (const rule of rules) {
    const issue = ruleCopyIssue(destination, source, rule, resources);
    if (issue) throw new Error(issue);
  }
  const cloneTargets = targets => structuredClone(targets).map(target => ({ ...target, rowKey: createKey() }));
  let ruleId = Math.max(0, ...destination.rules.map(rule => rule.id));
  let priority = destination.rules.length ? Math.max(...destination.rules.map(rule => Number(rule.priority))) : -1;
  const copiedRules = rules.map(rule => ({ ...structuredClone(rule), id: ++ruleId, profileId: destination.id, rowKey: createKey(), priority: ++priority, targets: cloneTargets(rule.targets) }));
  return { ...destination, rules: [...destination.rules, ...copiedRules] };
}
