export const CODEX_REASONING_EFFORT_OPTIONS = [
  { value: 'low', label: 'low', hint: 'Low' },
  { value: 'medium', label: 'medium', hint: 'Balanced' },
  { value: 'high', label: 'high', hint: 'High' },
  { value: 'xhigh', label: 'xhigh', hint: 'Extra High' },
  { value: 'max', label: 'max', hint: 'Max' },
  { value: 'ultra', label: 'ultra', hint: 'Ultra' }
];

const CODEX_REASONING_EFFORT_VALUES = new Set(
  CODEX_REASONING_EFFORT_OPTIONS.map((option) => option.value),
);

export const normalizeCodexReasoningEffort = (value, fallback = 'xhigh') => {
  const normalized = String(value || '').trim().toLowerCase();
  if (CODEX_REASONING_EFFORT_VALUES.has(normalized)) return normalized;
  return CODEX_REASONING_EFFORT_VALUES.has(fallback) ? fallback : 'xhigh';
};

export const getCodexReasoningEffortLoadState = (value) => {
  const sourceValue = String(value || '').trim().toLowerCase();
  const normalizedValue = normalizeCodexReasoningEffort(sourceValue);
  return {
    value: normalizedValue,
    initialValue: sourceValue || normalizedValue,
  };
};
