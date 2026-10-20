import { DetailChip as BaseDetailChip, DetailMetricCard as BaseDetailMetricCard, DetailPill as BaseDetailPill, DetailTimeoutPill as BaseDetailTimeoutPill, DetailTimestampFooter as BaseDetailTimestampFooter } from '@/features/providers/shared/endpoints/detailParts.jsx';

const DetailMetricCard = (props) => <BaseDetailMetricCard {...props} density="compact" />;

const DetailChip = (props) => <BaseDetailChip {...props} variant="codex" />;

const DetailPill = (props) => <BaseDetailPill {...props} variant="codex" />;

const DetailTimeoutPill = (props) => <BaseDetailTimeoutPill {...props} variant="codex" />;

const DetailTimestampFooter = (props) => <BaseDetailTimestampFooter {...props} density="compact" />;

export { DetailMetricCard, DetailChip, DetailPill, DetailTimeoutPill, DetailTimestampFooter };
