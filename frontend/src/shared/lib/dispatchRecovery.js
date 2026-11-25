export function dispatchRecoveryTarget(provider, candidate) {
  return {
    provider: provider || candidate.provider,
    channelId: Number(candidate.channelId),
    groupId: Number(candidate.groupId),
    endpointId: String(candidate.endpointId || ''),
    model: String(candidate.upstreamModel || '').trim() || String(candidate.model || '').trim(),
  };
}

function currentCandidate(states, target) {
  const matches = (states || []).filter(({ blockage }) => blockage
    && blockage.provider === target.provider
    && Number(blockage.channelId) === target.channelId
    && Number(blockage.groupId) === target.groupId
    && String(blockage.endpointId) === target.endpointId
    && (!target.model || blockage.model === target.model));
  if (matches.length !== 1) throw new Error('此端点的当前状态或模型范围已变化，请刷新后重试。');
  return matches[0];
}

export async function readCurrentDispatchCandidate(target, readRecovery) {
  const input = dispatchRecoveryTarget(target.provider, target);
  if (!input.endpointId) throw new Error('缺少端点编号，请刷新渠道资源。');
  return currentCandidate(await readRecovery(input), input);
}

export async function retryCurrentDispatchCandidate(target, { readRecovery, retryCandidate }) {
  const state = await readCurrentDispatchCandidate(target, readRecovery);
  if (!state.canRetry) return { released: false, state };
  // An empty snapshot model must not reopen a wider scope than the live check.
  const input = dispatchRecoveryTarget(state.blockage.provider, state.blockage);
  const updated = currentCandidate(await retryCandidate(input), input);
  return { released: true, state: updated };
}
