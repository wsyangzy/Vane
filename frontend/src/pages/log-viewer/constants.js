// 日志页面常量配置
import {
  Info,
  AlertTriangle,
  XCircle,
  CheckCircle,
} from 'lucide-react';

// 日志级别配置
export const LOG_LEVELS = {
  DEBUG: {
    color: 'text-slate-500',
    bg: 'bg-slate-50',
    ring: 'ring-slate-300',
    icon: Info
  },
  INFO: {
    color: 'text-blue-600',
    bg: 'bg-blue-50',
    ring: 'ring-blue-300',
    icon: CheckCircle
  },
  WARN: {
    color: 'text-amber-600',
    bg: 'bg-amber-50',
    ring: 'ring-amber-300',
    icon: AlertTriangle
  },
  ERROR: {
    color: 'text-red-600',
    bg: 'bg-red-50',
    ring: 'ring-red-300',
    icon: XCircle
  },
};

// 日志级别列表
export const LOG_LEVEL_LIST = ['DEBUG', 'INFO', 'WARN', 'ERROR'];
