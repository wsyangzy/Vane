import { getEndpointChainItems, getRequestResourcePath, parseChainNode } from './endpointChain.js';

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
      channelId: String(candidate.channel ?? ''),
      groupId: String(groupId ?? ''),
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
  const targets = (snapshot.decision.targets || []).map(target => {
    const channelId = String(target.channel_id ?? target.channelId ?? '');
    const groupId = String(target.group_id ?? target.groupId ?? '0');
    const targetProfileId = target.profile_id ?? target.profileId;
    const configuredModel = target.upstream_model || target.upstreamModel || '';
    if (targetProfileId > 0) return { label: `方案 #${targetProfileId}`, model: configuredModel, configuredModel };
    const channel = resources.find(item => item.provider === provider && String(item.id) === channelId);
    const historical = attempts.find(item => item.channelId === channelId && (groupId === '0' || item.groupId === groupId));
    const group = channel?.credentialGroups?.find(item => String(item.id) === groupId);
    return {
      label: [historical?.channel || channel?.name || `渠道 #${channelId}`, groupId === '0' ? '全部分组' : historical?.credentialGroup || group?.name || `分组 #${groupId}`].join(' / '),
      model: configuredModel || historical?.model || '',
      configuredModel,
    };
  });
  const source = snapshot.default_source || snapshot.defaultSource;
  let defaultSource = null;
  if (source && typeof source === 'object') {
    const channelId = String(source.channel_id ?? source.channelId ?? '');
    const groupId = String(source.group_id ?? source.groupId ?? '');
    const channel = resources.find(item => item.provider === provider && String(item.id) === channelId);
    const group = channel?.credentialGroups?.find(item => String(item.id) === groupId);
    defaultSource = {
      channelId,
      groupId,
      channel: source.channel_name || source.channelName || channel?.name || (channelId ? `渠道 #${channelId}` : ''),
      credentialGroup: source.group_name || source.groupName || group?.name || (groupId && groupId !== '0' ? `分组 #${groupId}` : ''),
      unavailable: !!source.unavailable,
    };
  }
  return {
    provider,
    requestId: snapshot.request_id || snapshot.requestId || request.requestId || request.request_id || '',
    profileId,
    profileName: snapshot.profile_name || snapshot.profileName || `方案 #${profileId}`,
    requestedModel: snapshot.requested_model || snapshot.requestedModel || '',
    defaultSource,
    strategy: snapshot.strategy || '',
    affinityMode: snapshot.affinity_mode || snapshot.affinityMode || '',
    decision: snapshot.decision,
    targets,
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

export const describeRequestRoutePath = (request = {}) => {
  const trace = request.dispatchTrace || readRequestDispatchTrace(request);
  const sourceModel = request.originalModelName || request.original_model_name || trace?.requestedModel || '';
  const targetModel = request.modelRewriteTarget || request.model_rewrite_target || request.model || '';
  // Destinations come only from sends. The saved default source explains where
  // the route began; it is not another upstream attempt.
  const nodes = trace?.attempts?.length ? trace.attempts : getRequestResourcePath({ ...request, dispatchTrace: trace });
  if (!nodes.length) {
    return !trace && sourceModel && targetModel && sourceModel !== targetModel ? `${sourceModel} → ${targetModel}` : '';
  }
  const paths = [];
  for (const node of nodes) {
    const actualModel = node.model || targetModel || sourceModel || '模型未记录';
    const modelLabel = sourceModel && sourceModel !== actualModel ? `${sourceModel} → ${actualModel}` : actualModel;
    const resource = [node.channel, node.credentialGroup].filter(Boolean).join(' / ') || '渠道未记录';
    const previous = paths.at(-1);
    if (previous?.model === modelLabel) {
      if (previous.resources.at(-1) !== resource) previous.resources.push(resource);
    } else paths.push({ model: modelLabel, resources: [resource] });
  }
  const snapshot = parseSnapshot(request.routeSnapshot || request.route_snapshot);
  const source = trace?.decision?.kind !== 'default' ? trace?.defaultSource : null;
  const origin = source?.unavailable ? '默认渠道不可用'
    : [source?.channel, source?.credentialGroup].filter(Boolean).join(' / ')
      || (!trace && snapshot?.kind ? snapshot.channel : '');
  if (origin && paths[0].resources[0] !== origin) paths[0].resources.unshift(origin);
  return paths.map(path => `${path.model}\n${path.resources.join(' → ')}`).join('\n\n');
};
