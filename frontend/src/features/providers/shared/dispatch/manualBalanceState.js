export function formatUsd(value) {
  const text = String(value ?? '0');
  if (!/^-?\d+(?:\.\d+)?$/.test(text)) return '—';
  const [whole, fraction = ''] = text.split('.');
  return `$${whole.replace(/\B(?=(\d{3})+(?!\d))/g, ',')}.${fraction.padEnd(2, '0')}`;
}

export function balanceAmountError(value, action) {
  const text = value.trim();
  if (!/^\d+(?:\.\d{0,9})?$/.test(text)) return '请输入非负美元金额，最多 9 位小数。';
  const [whole, fraction = ''] = text.split('.');
  const nanos = BigInt(whole) * 1_000_000_000n + BigInt(fraction.padEnd(9, '0'));
  if (nanos > 9_223_372_036_854_775_807n) return '金额超出支持范围。';
  if (action === 'credit' && nanos === 0n) return '充值金额需要大于 0。';
  return '';
}

export const balanceErrorMessage = (error) => {
  const message = error?.message || String(error);
  if (message.includes('balance_revision_conflict')) return '余额已被其他请求或操作更新。请关闭弹窗、读取最新余额后重新操作。';
  if (message.includes('balance_calibration_required')) return '凭证已变更或余额跟踪已关闭，请重新设置当前余额。';
  if (message.includes('balance_operation_conflict')) return '这次操作已生效或操作内容已改变。请关闭弹窗、读取最新余额后重新操作。';
  if (message.includes('balance_amount')) return '金额无效或超出支持范围，请检查输入。';
  return message;
};
