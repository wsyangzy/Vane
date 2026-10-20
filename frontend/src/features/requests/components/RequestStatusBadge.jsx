// ============================================
// RequestStatusBadge - 请求状态徽章
// 2025-12-01 09:54:42
// ============================================

import {
  Clock,
  ArrowRightCircle,
  Settings,
  RotateCw,
  Pause,
  CheckCircle2,
  XCircle,
  Ban,
  Timer
} from 'lucide-react';
import { getStatusConfig } from '@/shared/lib/constants.js';

// 图标映射表
const ICON_MAP = {
  Clock,
  ArrowRightCircle,
  Settings,
  RotateCw,
  Pause,
  CheckCircle2,
  XCircle,
  Ban,
  Timer
};

/**
 * RequestStatusBadge - 请求状态徽章组件
 * @param {Object} props
 * @param {string} props.status - 状态值
 */
const RequestStatusBadge = ({ status }) => {
  const config = getStatusConfig(status);
  const IconComponent = ICON_MAP[config.icon];

  return (
    <div className={`inline-flex items-center gap-1.5 px-2 py-0.5 rounded text-xs font-medium border ${config.color}`}>
      {IconComponent && <IconComponent className="w-3 h-3" />}
      {config.label}
    </div>
  );
};

export default RequestStatusBadge;
