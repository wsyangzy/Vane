export const escapeRegex = (text) => text.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');

const logEmoji = /(?:[#*0-9]\uFE0F?\u20E3|\p{Regional_Indicator}{2}|\p{Extended_Pictographic}(?:[\uFE0E\uFE0F]|\p{Emoji_Modifier})?(?:\u200D\p{Extended_Pictographic}(?:[\uFE0E\uFE0F]|\p{Emoji_Modifier})?)*(?:[\u{E0020}-\u{E007E}]+\u{E007F})?)([ \t]*)/gu;

export const cleanLogMessage = (message) => message.replace(logEmoji, (match, spaces, offset, source) => {
  const symbol = match.slice(0, match.length - spaces.length);
  if (/^[©®™]$/u.test(symbol)) return match;
  // Keep indentation and separators, including text symbols used without emoji styling.
  return spaces && offset > 0 && !/\s/u.test(source[offset - 1]) ? spaces : '';
});

export const normalizeLogEntry = (log) => {
  if (!log || typeof log.message !== 'string') return log;
  const message = cleanLogMessage(log.message);
  return message === log.message ? log : { ...log, message };
};

export const formatLogValue = (value) => {
  if (typeof value === 'string') return value;
  if (value === undefined) return 'undefined';
  return JSON.stringify(value);
};

export const formatLogTime = (timestamp, full = false) => {
  const date = new Date(timestamp);
  if (!timestamp || !Number.isFinite(date.getTime())) return '—';
  const options = { hour12: false, hour: '2-digit', minute: '2-digit', second: '2-digit', fractionalSecondDigits: 3 };
  if (full) Object.assign(options, { year: 'numeric', month: '2-digit', day: '2-digit' });
  return date.toLocaleString('zh-CN', options);
};

export const formatLogText = (log) => {
  const fields = Object.entries(log.attrs || {}).map(([key, value]) => `${key}=${formatLogValue(value)}`);
  const text = `[${formatLogTime(log.timestamp, true)}] [${log.level || 'INFO'}] ${cleanLogMessage(String(log.message ?? ''))}`;
  return fields.length ? `${text} | ${fields.join(' ')}` : text;
};
