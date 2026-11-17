import { Pencil } from 'lucide-react';
import { formatMultiplierExpression, parseMultiplierExpression } from '@/shared/lib/multiplier.js';
import { isSubscriptionGroup } from './subscriptions/subscriptionSettings.js';

export default function ChannelSetupReview({ channel, group, endpoint, onEdit, loading, children }) {
  const subscription = isSubscriptionGroup(group);
  const rows = [
    { label: '渠道', value: channel.name, notes: [channel.website, channel.model && `模型：${channel.model}`], step: 0 },
    { label: '凭证分组', value: group.name, notes: [subscription ? `官方订阅 · ${group.subscription?.accountId ? '托管账号' : '客户端凭证透传'}` : '访问凭证已配置'], step: 1 },
    { label: '成本与限额', value: group.billingRules?.length ? `组合计费 · ${group.billingRules.length} 条规则` : `总倍率 ${formatMultiplierExpression(parseMultiplierExpression(group.costMultiplier), 3)}×`, notes: [Number(group.dailyCostLimitUSD) > 0 ? `每日成本限额 $${group.dailyCostLimitUSD}` : '每日成本不限额'], step: 2 },
    { label: '首个端点', value: endpoint.name, notes: [endpoint.baseURL || endpoint.url], step: 3 },
  ];
  return <div className="channel-setup-review-layout">
    <div className="channel-setup-review-summary">
    <dl className="channel-setup-review">{rows.map(row => <div key={row.label}>
      <dt>{row.label}</dt>
      <dd><div className="channel-setup-review-content"><span className="channel-setup-review-value">{row.value}</span>{row.notes.map((note, index) => note && <small key={index}>{note}</small>)}</div><button type="button" onClick={() => onEdit(row.step)} disabled={loading} aria-label={`修改${row.label}`}><Pencil size={12} />修改</button></dd>
    </div>)}</dl>
    </div>
    <div className="channel-setup-review-checks">{children}</div>
  </div>;
}
