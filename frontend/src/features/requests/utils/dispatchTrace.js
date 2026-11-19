import { getEndpointChainItems, getRequestResourcePath, isCodexSearchRequest, parseChainNode } from './endpointChain.js';
import { mapBlockage } from './blockage.js';

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
  const events = Array.isArray(snapshot.candidates) ? snapshot.candidates : [];
  const history = getEndpointChainItems(request).map((item, index) => parseChainNode(item, index + 1)).filter(node => !node.skipReason);
  // Both records follow send order. Only use that order to recover historical
  // names without a resource match when all sends are accounted for.
  const alignedHistory = history.length > 0 && history.every(node => node.attempts > 0)
    && history.reduce((count, node) => count + node.attempts, 0) === events.filter(event => event?.action === 'attempt' && event.candidate).length;
  const resolve = (candidate, index) => {
    const channel = resources.find(item => item.provider === provider && String(item.id) === String(candidate.channel));
    const endpointId = String(candidate.endpoint || '');
    const endpoint = channel?.endpoints?.find(item => String(item.id) === endpointId);
    const key = parseChainNode(endpointId);
    // A historical hop can cover several retries on the same endpoint.
    let offset = index;
    let aggregateLastAttempt = true;
    const old = index < 0 ? null : history.find(node => {
      const count = Math.max(1, node.attempts || 0);
      if (offset < count) {
        aggregateLastAttempt = offset === count - 1;
        return true;
      }
      offset -= count;
      return false;
    });
    const groupId = (String(candidate.credential || '').match(/^\d+\/(\d+)$/)?.[1]) ?? (endpointId.match(/::g(\d+)::/)?.[1]) ?? endpoint?.credentialGroupId ?? endpoint?.credential_group_id;
    const group = (channel?.credentialGroups || []).find(item => String(item.id) === String(groupId));
    // The old chain carries names captured during execution. Match it to the
    // resolved identity before using it; repeated attempts keep separate rows.
    const endpointName = endpoint?.name || (endpointId.includes('::') ? key.endpoint : /^\d+$/.test(endpointId) ? '' : endpointId);
    const matchesHistory = old && (alignedHistory ||
      (old.endpoint === endpointName || old.endpoint === endpointId) && (!channel || !old.channel || old.channel === channel.name) && (!group || !old.credentialGroup || old.credentialGroup === group.name)
      || history.length === 1 && old.channel === request.channel && old.endpoint === request.endpoint
    );
    const captured = matchesHistory ? old : null;
    return {
      // Grouped hop diagnostics are totals, not individual attempt timings or
      // statuses. Keep their names without assigning those totals to a retry.
      ...(captured?.attempts > 1 ? (aggregateLastAttempt ? { error: captured.error, local: captured.local } : {}) : captured),
      channelId: String(candidate.channel ?? ''),
      groupId: String(groupId ?? ''),
      endpointId,
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
  for (const event of events) {
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
      const blockage = mapBlockage(event.blockage);
      skipped.push({ ...resolve(candidate, -1), reason: event.reason || '', blockage,
        ...(blockage?.channelName ? { channel: blockage.channelName } : {}),
        ...(blockage?.groupName ? { credentialGroup: blockage.groupName } : {}),
        ...(blockage?.endpointName ? { endpoint: blockage.endpointName } : {}),
        ...(blockage?.groupId ? { groupId: String(blockage.groupId) } : {}) });
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
        if (event.blockage) attempt.blockage = mapBlockage(event.blockage);
      }
    }
  }
  const rawCount = Number(snapshot.attempts);
  const targets = (snapshot.decision.targets || []).map(target => {
    const channelId = String(target.channel_id ?? target.channelId ?? '');
    const groupId = String(target.group_id ?? target.groupId ?? '0');
    const targetProfileId = target.profile_id ?? target.profileId;
    const configuredModel = target.upstream_model || target.upstreamModel || '';
    // Profile references expand to members the snapshot does not record, so
    // their execution cannot be attributed from candidate identities.
    if (targetProfileId > 0) return { label: `方案 #${targetProfileId}`, model: configuredModel, configuredModel, attempted: null, skipReasons: [] };
    // A target channel can also be a default member; its configured model
    // separates a rule send from a default-stage send to the same group.
    const matches = item => item.channelId === channelId && (groupId === '0' || item.groupId === groupId) && (!configuredModel || item.model === configuredModel);
    const channel = resources.find(item => item.provider === provider && String(item.id) === channelId);
    const historical = attempts.find(item => item.channelId === channelId && (groupId === '0' || item.groupId === groupId));
    const group = channel?.credentialGroups?.find(item => String(item.id) === groupId);
    return {
      label: [historical?.channel || channel?.name || `渠道 #${channelId}`, groupId === '0' ? '全部分组' : historical?.credentialGroup || group?.name || `分组 #${groupId}`].join(' / '),
      model: configuredModel || historical?.model || '',
      configuredModel,
      attempted: attempts.some(matches),
      skipReasons: [...new Set(skipped.filter(matches).map(item => item.reason).filter(Boolean))],
    };
  });
  // null: undeterminable (default orchestration or profile-reference targets).
  const ruleExecuted = snapshot.decision.kind === 'default' || !targets.length || targets.some(item => item.attempted === null)
    ? null
    : targets.some(item => item.attempted);
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
    ruleExecuted,
    decisionLabel: dispatchDecisionLabel(snapshot.decision.kind),
    attemptCount: Math.max(attempts.length, Number.isFinite(rawCount) ? rawCount : 0),
    attempts,
    skipped,
    diagnostics: [...diagnostics.values()],
    stopReason: snapshot.stop_reason || snapshot.stopReason || '',
    waitMs: Math.max(0, Number(snapshot.wait_ms ?? snapshot.waitMs) || 0),
    maxAttempts: Math.max(0, Number(snapshot.max_attempts ?? snapshot.maxAttempts) || 0),
    maxDurationMs: Math.max(0, Number(snapshot.max_duration_ms ?? snapshot.maxDurationMs) || 0),
    nextRecoveryAt: snapshot.next_recovery_at || snapshot.nextRecoveryAt || '',
    successful: !!snapshot.successful,
    responseStarted: !!(snapshot.response_started ?? snapshot.responseStarted),
    createdAt: snapshot.created_at || snapshot.createdAt || '',
    finishedAt: snapshot.finished_at || snapshot.finishedAt || '',
    finished: !!(snapshot.finished_at || snapshot.finishedAt),
  };
}

export const summarizeRequestRoute = (request = {}) => {
  const trace = request.dispatchTrace || readRequestDispatchTrace(request);
  const sourceModel = request.originalModelName || request.original_model_name || trace?.requestedModel || '';
  const targetModel = request.modelRewriteTarget || request.model_rewrite_target || request.model || '';
  // Destinations come only from sends. The saved default source explains where
  // the route began; it is not another upstream attempt.
  const nodes = trace?.attempts?.length && !isCodexSearchRequest(request)
    ? trace.attempts
    : getRequestResourcePath({ ...request, dispatchTrace: trace });
  const modelRewrite = !trace && sourceModel && targetModel && sourceModel !== targetModel ? [sourceModel, targetModel] : [];
  if (!nodes.length) return { paths: [], resources: [], models: modelRewrite, sourceModel };
  const paths = [];
  const resources = [];
  const models = [];
  for (const node of nodes) {
    const actualModel = node.model || targetModel || sourceModel || '模型未记录';
    const modelLabel = sourceModel && sourceModel !== actualModel ? `${sourceModel} → ${actualModel}` : actualModel;
    const resource = (node.channel === '搜索服务' ? [node.channel, node.endpoint] : [node.channel, node.credentialGroup])
      .filter(Boolean).join(' / ') || '渠道未记录';
    if (resources.at(-1) !== resource) resources.push(resource);
    if (actualModel !== '模型未记录' && models.at(-1) !== actualModel) models.push(actualModel);
    const previous = paths.at(-1);
    if (previous?.model === modelLabel) {
      if (previous.resources.at(-1) !== resource) previous.resources.push(resource);
    } else paths.push({ model: modelLabel, resources: [resource] });
  }
  const snapshot = trace ? null : parseSnapshot(request.routeSnapshot || request.route_snapshot);
  const source = trace?.decision?.kind !== 'default' ? trace?.defaultSource : null;
  const origin = source?.unavailable ? '默认渠道不可用'
    : [source?.channel, source?.credentialGroup].filter(Boolean).join(' / ')
      || (!trace && snapshot?.kind ? snapshot.channel : '');
  if (origin && resources[0] !== origin) {
    resources.unshift(origin);
    paths[0].resources.unshift(origin);
  }
  return { paths, resources, models: models.length ? models : modelRewrite, sourceModel };
};

export const describeRequestRoutePath = (request, route = summarizeRequestRoute(request)) => {
  if (!route.paths.length) return route.models.join(' → ');
  return route.paths.map(path => `${path.model}\n${path.resources.join(' → ')}`).join('\n\n');
};
