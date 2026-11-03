import { readRequestDispatchTrace } from './dispatchTrace.js';

// Older Codex rows could archive a clean EOF as success without a completion
// event. Their finished dispatch snapshot still records the actual outcome.
export const getRequestOutcome = (request = {}) => {
  const status = String(request.status || '').toLowerCase();
  const stored = request.streamOutcome || request.stream_outcome || '';
  if (status !== 'completed') return stored;
  const trace = request.dispatchTrace || readRequestDispatchTrace(request);
  if (trace?.finished && !trace.successful) {
    const reason = trace.attempts?.at(-1)?.reason || trace.stopReason;
    if (['incomplete_response', 'response_incomplete'].includes(reason)) return 'incomplete';
    if (trace.stopReason === 'client_cancelled') return 'canceled';
    return 'failed';
  }
  const reason = request.failureReason || request.failure_reason;
  if (['incomplete_stream', 'stream_truncated'].includes(reason)) return 'incomplete';
  return stored;
};

export const getRequestDisplayStatus = (request = {}) => {
  if (request.status !== 'completed') return request.status;
  const outcome = getRequestOutcome(request);
  return ({ incomplete: 'incomplete', failed: 'failed', canceled: 'cancelled' })[outcome] || request.status;
};
