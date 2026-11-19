import { isHealthCooldown, mapBlockage } from './blockage.js';
import { skippedCandidates } from './failureSummary.js';

export function collectRecoveryGroups(requests) {
  const groups = new Map();
  for (const request of requests) {
    if (request.status !== 'failed') continue;
    for (const event of skippedCandidates(request)) {
      if (!isHealthCooldown(event.reason)) continue;
      const blockage = mapBlockage(event.blockage);
      const provider = blockage?.provider || request.provider;
      const channelId = blockage?.channelId || Number(event.channelId || event.channel);
      const groupId = blockage?.groupId || Number(event.groupId || String(event.credential || '').match(/^\d+\/(\d+)$/)?.[1]);
      const endpointId = blockage?.endpointId || event.endpointId || event.endpoint;
      const model = blockage?.model || event.model;
      if (!provider || !channelId || !groupId || !endpointId || !model) continue;
      const key = JSON.stringify([provider, channelId, groupId, model]);
      if (!groups.has(key)) {
        groups.set(key, { key, provider, channelId, groupId, model, endpoints: new Set(),
          label: [blockage?.channelName || event.channel, blockage?.groupName || event.credentialGroup]
            .filter(Boolean).join(' / ') || `渠道 #${channelId} / 分组 #${groupId}` });
      }
      groups.get(key).endpoints.add(String(endpointId));
    }
  }
  return [...groups.values()];
}

export const recoveryCandidateKey = (blockage) => JSON.stringify([
  blockage.provider, blockage.channelId, blockage.groupId, blockage.model, blockage.endpointId,
]);

export async function readRecoveryCandidates(groups, getRecovery) {
  const entries = [];
  for (const group of groups) {
    try {
      const current = await getRecovery(group);
      const candidates = current.filter(item => group.endpoints.has(item.blockage?.endpointId));
      entries.push({ group, candidates, missing: group.endpoints.size - candidates.length });
    } catch (error) {
      entries.push({ group, candidates: [], error: error?.message || '无法读取当前状态' });
    }
  }
  return entries;
}

export const releasableCandidates = (entries) => entries.flatMap(({ candidates }) => candidates
  .filter(({ blockage, canRetry }) => canRetry && isHealthCooldown(blockage.reason))
  .map(({ blockage }) => blockage));

export async function releaseRecoveryCandidates(groups, selected, { getRecovery, retryCandidate }) {
  const selectedKeys = new Set(selected.map(recoveryCandidateKey));
  const before = await readRecoveryCandidates(groups, getRecovery);
  const failures = new Map();
  let released = 0;
  for (const candidate of releasableCandidates(before)) {
    const key = recoveryCandidateKey(candidate);
    if (!selectedKeys.has(key)) continue;
    try {
      await retryCandidate(candidate);
      released++;
    } catch (error) {
      failures.set(key, error?.message || '解除冷却失败');
    }
  }
  const entries = await readRecoveryCandidates(groups, getRecovery);
  for (const { candidates } of entries) {
    for (const { blockage } of candidates) {
      // Endpoints may share a health gate. A previous call can open it for
      // later candidates before their turn reaches the live retry check.
      if (!blockage.reason || blockage.recovery === 'probing') failures.delete(recoveryCandidateKey(blockage));
    }
  }
  const checked = new Set(before.flatMap(({ candidates }) => candidates.map(({ blockage }) => recoveryCandidateKey(blockage))));
  return { entries, released, failures, missing: [...selectedKeys].filter(key => !checked.has(key)).length };
}
