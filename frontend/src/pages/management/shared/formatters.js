export const getErrorMessage = (error, fallback = '操作失败') => {
  if (!error) return fallback;
  if (typeof error === 'string') return error || fallback;
  if (error instanceof Error) return error.message || fallback;
  if (typeof error?.message === 'string' && error.message) return error.message;
  try {
    return JSON.stringify(error);
  } catch {
    return String(error);
  }
};

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
