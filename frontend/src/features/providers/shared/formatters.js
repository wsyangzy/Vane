import { formatMultiplierExpression, parseMultiplierExpression } from '@/shared/lib/multiplier.js';
import { getErrorMessage } from '@/shared/lib/errorMessage.js';

export { getErrorMessage };

export const FAILOVER_CANDIDATE_REQUIRED_MESSAGE = '至少保留一个参与转移的备用端点，已保留当前端点。';

export const isFailoverCandidateRequiredError = (error) => {
  const message = getErrorMessage(error, '');
  return message.includes('没有其他参与故障转移') ||
    message.includes('没有其他参与渠道内故障转移');
};

export const getFailoverToggleErrorMessage = (error, fallback = '切换故障转移失败') => (
  isFailoverCandidateRequiredError(error)
    ? FAILOVER_CANDIDATE_REQUIRED_MESSAGE
    : getErrorMessage(error, fallback)
);

export const formatLastCheck = (time) => {
  if (!time || time === '-') return '-';
  try {
    const date = new Date(time);
    return date.toLocaleString('zh-CN', {
      month: '2-digit',
      day: '2-digit',
      hour: '2-digit',
      minute: '2-digit'
    });
  } catch {
    return String(time);
  }
};

export const getDetailTimestamp = (...values) => {
  for (const value of values) {
    const text = String(value ?? '').trim();
    if (text && text !== '-' && !text.startsWith('0001-01-01')) {
      return text;
    }
  }
  return '-';
};

export const normalizeDetailMultiplier = (value) => {
  const parsed = typeof value === 'string'
    ? parseMultiplierExpression(value, Number.NaN)
    : Number(value);
  return Number.isFinite(parsed) ? parsed : 1;
};

export const isDefaultDetailMultiplier = (value) => Math.abs(normalizeDetailMultiplier(value) - 1) <= 0.000001;

export const formatDetailMultiplier = (value) => `${formatMultiplierExpression(normalizeDetailMultiplier(value), 3)}x`;
