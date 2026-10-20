import SharedSettings, { SubscriptionStatus as SharedStatus } from '@/features/providers/shared/subscriptions/SubscriptionSettings.jsx';
export default function SubscriptionSettings(props) { return <SharedSettings {...props} provider="claude" />; }
export function SubscriptionStatus(props) { return <SharedStatus {...props} />; }
