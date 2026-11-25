const ACTIONS = { redact: '脱敏', detect: '仅检测', block: '历史拦截' };

export function getPrivacyActionLabel(item, hit) {
  if (hit.action === 'redact' && item.state !== 'redacted') {
    return item.state === 'detected' ? '已命中' : '命中 · 未确认替换';
  }
  return ACTIONS[hit.action] || '已命中';
}
