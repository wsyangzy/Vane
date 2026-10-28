export const isSubscriptionGroup = (group) => (group?.authMode || group?.auth_mode) === 'client_passthrough';

export const subscriptionStateLabel = (state) => ({
  paused: '额度暂停', due_unverified: '到期，待真实请求验证', probing: '正在验证恢复',
  manual_disabled: '已手动暂停官方', unknown: '状态暂不可用'
}[state] || state);

export function validateSubscription(form) {
  if (form.authMode !== 'client_passthrough') return '';
  if (!form.subscription) return '请选择订阅凭证来源';
  return '';
}
