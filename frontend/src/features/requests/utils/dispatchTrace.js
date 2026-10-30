import { getEndpointChainItems, parseChainNode } from './endpointChain.js';

const parseSnapshot = (value) => {
  if (typeof value === 'string') {
    try { value = JSON.parse(value); } catch { return null; }
  }
  return value && typeof value === 'object' && !Array.isArray(value) ? value : null;
};
const identity = (candidate) => JSON.stringify([candidate.provider, candidate.channel, candidate.credential, candidate.endpoint, candidate.model, candidate.protocol, candidate.state_version ?? candidate.stateVersion]);

export const dispatchDecisionLabel = (kind) => ({ default: '默认编排', model: '模型路由', mapping: '模型映射', non_stream: '非流改道', composite: '条件路由', intent: '意图路由', explicit: '指定资源' }[kind] || '方案调度');

// Current resources resolve names only. Historical decisions and counts always
// come from the request snapshot, even when a profile is edited or deleted.
export function readRequestDispatchTrace(request, resources = []) {
  const snapshot = parseSnapshot(request.routeSnapshot || request.route_snapshot);
  const profileId = snapshot?.profile_id ?? snapshot?.profileId;
  if (!profileId || !snapshot.decision?.kind) return null;
  const provider = snapshot.provider || request.provider;
  const history = getEndpointChainItems(request).map((item, index) => parseChainNode(item, index + 1));
  const resolve = (candidate, index) => {
    const channel = resources.find(item => item.provider === provider && String(item.id) === String(candidate.channel));
    const endpointId = String(candidate.endpoint || '');
    const endpoint = channel?.endpoints?.find(item => String(item.id) === endpointId);
    const key = parseChainNode(endpointId);
    // A historical hop can cover several retries on the same endpoint.
    let offset = index;
    const old = index < 0 ? null : history.find(node => {
      offset -= Math.max(1, node.attempts || 0);
      return offset < 0;
    });
    const groupId = (String(candidate.credential || '').match(/^\d+\/(\d+)$/)?.[1]) ?? (endpointId.match(/::g(\d+)::/)?.[1]) ?? endpoint?.credentialGroupId ?? endpoint?.credential_group_id;
    const group = (channel?.credentialGroups || []).find(item => String(item.id) === String(groupId));
    // The old chain carries names captured during execution. Match it to the
    // resolved identity before using it; repeated attempts keep separate rows.
    const endpointName = endpoint?.name || (endpointId.includes('::') ? key.endpoint : /^\d+$/.test(endpointId) ? '' : endpointId);
    const matchesHistory = old && (
      (old.endpoint === endpointName || old.endpoint === endpointId) && (!channel || !old.channel || old.channel === channel.name) && (!group || !old.credentialGroup || old.credentialGroup === group.name)
      || history.length === 1 && old.channel === request.channel && old.endpoint === request.endpoint
    );
    const captured = matchesHistory ? old : null;
    return {
      // Grouped hop diagnostics are totals, not individual attempt timings or
      // statuses. Keep their names without assigning those totals to a retry.
      ...(captured?.attempts > 1 ? {} : captured),
      channel: captured?.channel || channel?.name || key.channel || `渠道 #${candidate.channel || '未知'}`,
      credentialGroup: captured?.credentialGroup || group?.name || (groupId && groupId !== '0' ? `分组 #${groupId}` : '分组未记录'),
      endpoint: captured?.endpoint || endpointName || (endpointId ? `端点 #${endpointId}` : '端点未记录'),
      model: candidate.model || request.model || '',
      protocol: candidate.protocol || '',
    };
  };
  const attempts = [];
  const latestAttempts = new Map();
  const skipped = [];
  const diagnostics = new Map();
  for (const event of Array.isArray(snapshot.candidates) ? snapshot.candidates : []) {
    const candidate = event?.candidate;
    if (!candidate || typeof candidate !== 'object') continue;
    const key = identity(candidate);
    if (!diagnostics.has(key)) {
      const credential = String(candidate.credential || '');
      const staticCredential = credential.match(/^\d+\/(\d+)$/);
      const version = candidate.state_version ?? candidate.stateVersion;
      diagnostics.set(key, {
        ...resolve(candidate, event.action === 'attempt' ? attempts.length : -1),
        channelId: String(candidate.channel ?? ''),
        groupId: staticCredential?.[1] || String(candidate.endpoint || '').match(/::g(\d+)::/)?.[1] || '',
        // Managed identities are opaque account hashes. Retain the diagnosis
        // without copying those identifiers into the display model.
        credentialLabel: staticCredential ? credential : credential.startsWith('account:') ? '托管账号（标识已隐藏）' : credential ? '凭证标识已隐藏' : '未记录',
        endpointId: String(candidate.endpoint ?? ''),
        stateVersion: version == null ? null : String(version),
        events: [],
      });
    }
    diagnostics.get(key).events.push({ action: event.action || '', reason: event.reason || '', scope: event.failure_scope || '' });
    if (event.action === 'skipped') {
      skipped.push({ ...resolve(candidate, -1), reason: event.reason || '' });
    } else if (event.action === 'attempt') {
      const attempt = { ...resolve(candidate, attempts.length), key: String(attempts.length), action: 'attempt', reason: '', hop: attempts.length + 1, events: [{ action: 'attempt' }] };
      attempts.push(attempt);
      latestAttempts.set(key, attempt);
    } else {
      const attempt = latestAttempts.get(key);
      if (attempt) {
        attempt.action = event.action;
        attempt.reason = event.reason || '';
        attempt.scope = event.failure_scope || '';
        attempt.events.push({ action: event.action, reason: event.reason || '', scope: event.failure_scope || '' });
      }
    }
  }
  const rawCount = Number(snapshot.attempts);
  return {
    provider,
    requestId: snapshot.request_id || snapshot.requestId || request.requestId || request.request_id || '',
    profileId,
    profileName: snapshot.profile_name || snapshot.profileName || `方案 #${profileId}`,
    strategy: snapshot.strategy || '',
    affinityMode: snapshot.affinity_mode || snapshot.affinityMode || '',
    decision: snapshot.decision,
    decisionLabel: dispatchDecisionLabel(snapshot.decision.kind),
    attemptCount: Math.max(attempts.length, Number.isFinite(rawCount) ? rawCount : 0),
    attempts,
    skipped,
    diagnostics: [...diagnostics.values()],
    stopReason: snapshot.stop_reason || snapshot.stopReason || '',
    waitMs: Math.max(0, Number(snapshot.wait_ms ?? snapshot.waitMs) || 0),
    successful: !!snapshot.successful,
    responseStarted: !!(snapshot.response_started ?? snapshot.responseStarted),
    createdAt: snapshot.created_at || snapshot.createdAt || '',
    finishedAt: snapshot.finished_at || snapshot.finishedAt || '',
    finished: !!(snapshot.finished_at || snapshot.finishedAt),
  };
}
