import SharedSettings, { SubscriptionStatus as SharedStatus } from '@pages/management/shared/SubscriptionSettings.jsx';
export default function SubscriptionSettings(props) { return <SharedSettings {...props} provider="claude" />; }
export function SubscriptionStatus(props) { return <SharedStatus {...props} />; }
