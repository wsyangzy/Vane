import { buildFailureToast, describeFailureReason, extractFailureCode, getRequestFailureDetails, shouldNotifyFailure } from './failureSummary.js';

const requestID = (request) => String(request.rawRequestId || request.requestId || request.request_id || request.id || '');
const snapshotOf = (request) => {
  const value = request.routeSnapshot || request.route_snapshot;
  if (value && typeof value === 'object') return value;
  try {
    const parsed = JSON.parse(value || '{}');
    return parsed && typeof parsed === 'object' && !Array.isArray(parsed) ? parsed : {};
  } catch { return {}; }
};

const failureScope = (request) => {
  const snapshot = snapshotOf(request);
  const profile = request.dispatchTrace?.profileId || snapshot.profile_id || snapshot.profileId;
  if (profile) {
    const decision = request.dispatchTrace?.decision || snapshot.decision || {};
    return ['profile', profile, decision.kind || '', decision.rule_id || decision.ruleId || '', decision.intent_id || decision.intentId || ''];
  }
  return ['route', snapshot.kind || '', snapshot.id || '', snapshot.channel || request.channel || '', request.group || request.group_name || '', request.endpoint || request.endpoint_name || ''];
};

const blockerScope = (blockers) => [...new Set(blockers.map(item => JSON.stringify([
  item.reason, item.blockage?.cause || '', item.blockage?.channelId || item.channel, item.blockage?.groupId || item.credentialGroup,
])))].sort();

// Keep a group alive while same-cause failures keep arriving. A quiet period
// starts a new notification window; updating a dismissed toast does not reopen it.
export const createFailureNotificationAggregator = ({ windowMs = 30000, maxGroups = 256 } = {}) => {
  const groups = new Map();
  let sequence = 0;
  return {
    consume(requests, now = Date.now()) {
      for (const [key, group] of groups) {
        if (now >= group.expiresAt) groups.delete(key);
      }
      const changes = new Map();
      for (const request of requests) {
        const reason = request.failure_reason || request.failureReason || '';
        if (extractFailureCode(reason) === 'client_cancelled') continue;
        const details = getRequestFailureDetails(request);
        if (!shouldNotifyFailure(reason) && !details.cooldownBlocked && !details.blockingCandidates.length) continue;
        const scope = failureScope(request);
        // Candidate and rule differences do not change the action for a fully
        // cooldown-blocked request on the same dispatch profile.
        const coolingScope = scope[0] === 'profile' ? scope.slice(0, 2) : scope;
        const key = JSON.stringify([String(request.provider || 'claude').toLowerCase(), details.cooldownBlocked
          ? [coolingScope, 'unsent_cooldown']
          : [scope, extractFailureCode(reason) || describeFailureReason(reason), blockerScope(details.blockingCandidates)]]);
        let group = groups.get(key);
        const existing = Boolean(group);
        if (!group) {
          group = { id: `request-failure-${++sequence}`, count: 0, expiresAt: now + windowMs };
          groups.set(key, group);
          if (groups.size > maxGroups) groups.delete(groups.keys().next().value);
        }
        group.count++;
        const recoveryAt = details.cooldownBlocked ? Date.parse(details.nextRecoveryAt) || 0 : 0;
        group.expiresAt = Math.max(group.expiresAt, now + windowMs, recoveryAt);
        const failure = buildFailureToast(request, details, now);
        changes.set(key, {
          id: group.id,
          update: changes.get(key)?.update ?? existing,
          ...failure,
          message: [...failure.message, ...(group.count > 1 ? [`已合并 ${group.count} 个请求`] : [])],
        });
      }
      return [...changes.values()];
    },
  };
};

// Read a fixed completion-time window before advancing the cursor. A page cap
// bounds each poll's work; an unfinished window resumes on the following poll.
// Initial records seed deduplication only, so opening Vane does not replay old
// failures. The first window is read again to catch requests that finish while
// that initial snapshot is being taken.
export const createFailureNotificationPoller = ({ fetchRequests, now = Date.now, pageSize = 100, pagesPerPoll = 5, maxSeen = 20000 }) => {
  const seen = new Map();
  let cursor = null;
  let pending = null;
  let initialized = false;
  let seedOverflow = false;
  const remember = (id) => {
    if (!id || seen.has(id)) return false;
    seen.set(id, true);
    if (seen.size > maxSeen) {
      seen.delete(seen.keys().next().value);
      if (pending?.seed) seedOverflow = true;
    }
    return true;
  };
  return {
    async poll() {
      const currentSecond = Math.floor(now() / 1000) * 1000;
      if (cursor === null) cursor = currentSecond;
      if (!pending) {
        const before = initialized ? currentSecond : currentSecond + 1000;
        if (before <= cursor) return [];
        pending = { since: cursor, before, page: 1, seed: !initialized };
      }
      const fresh = [];
      for (let index = 0; index < pagesPerPoll; index++) {
        let data;
        try {
          data = await fetchRequests({ status: 'failed', page: pending.page, pageSize,
            completedSince: new Date(pending.since).toISOString().replace('.000Z', 'Z'),
            completedBefore: new Date(pending.before).toISOString().replace('.000Z', 'Z') });
        } catch {
          // Completed pages have already been consumed; retry only the failed
          // page next time, retaining these results for the current notification.
          return fresh;
        }
        const requests = data?.requests;
        if (!Array.isArray(requests)) return fresh;
        for (const request of requests) {
          if (remember(requestID(request)) && !pending.seed) fresh.push(request);
        }
        if (requests.length < pageSize) {
          if (pending.seed) {
            initialized = true;
            // An exceptionally large initial snapshot remains a baseline;
            // never replay evicted historical IDs as new failures.
            if (seedOverflow) { cursor = pending.before; seen.clear(); }
          }
          else { cursor = pending.before; seen.clear(); }
          pending = null;
          break;
        }
        pending.page++;
      }
      return fresh;
    },
  };
};
