export const isSubscriptionGroup = (group) => (group?.authMode || group?.auth_mode) === 'client_passthrough';

export async function generateLocalRelayKey(cryptoProvider = globalThis.crypto) {
  const bytes = cryptoProvider.getRandomValues(new Uint8Array(32));
  const key = `relay_${Array.from(bytes, (value) => value.toString(16).padStart(2, '0')).join('')}`;
  const digest = await cryptoProvider.subtle.digest('SHA-256', new TextEncoder().encode(key));
  return { key, hash: Array.from(new Uint8Array(digest), (value) => value.toString(16).padStart(2, '0')).join('') };
}

export const subscriptionStateLabel = (state) => ({
  paused: '额度暂停', due_unverified: '到期，待真实请求验证', probing: '正在验证恢复',
  manual_disabled: '已手动暂停官方', unknown: '状态暂不可用'
}[state] || state);

export function validateSubscription(form) {
  if (form.authMode !== 'client_passthrough') return '';
  if (!form.subscription?.keyHash && !form.localKeyConfigured) return '请生成调用方本地密钥';
  if (form.subscription?.keyHash && !form.localKeyAcknowledged) return '请先保存新密钥并勾选确认；关闭弹窗后无法再次查看';
  if ((form.subscription?.backupTargets || []).some((target) => !Number(target.channelId) || !Number(target.groupId))) return '每个备用目标都必须选择普通凭证分组';
  return '';
}
