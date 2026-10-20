// ============================================
// ConfigItem - 配置项组件
// 2025-12-01
// ============================================

const ConfigItem = ({ label, value, type = 'text' }) => {
  // 格式化纳秒为可读时间格式
  const formatDuration = (ns) => {
    const seconds = ns / 1000000000;
    if (seconds < 1) return `${Math.round(ns / 1000000)}ms`;
    if (seconds < 60) return `${Math.round(seconds)}s`;
    if (seconds < 3600) return `${Math.round(seconds / 60)}m`;
    return `${Math.round(seconds / 3600)}h`;
  };

  const renderValue = () => {
    if (value === undefined || value === null) {
      return <span className="text-slate-400">-</span>;
    }

    if (type === 'boolean') {
      return (
        <span className={`px-2 py-0.5 rounded text-xs font-medium ${
          value ? 'bg-emerald-100 text-emerald-700' : 'bg-slate-100 text-slate-600'
        }`}>
          {value ? '启用' : '禁用'}
        </span>
      );
    }

    if (type === 'duration') {
      // 如果是数字，认为是纳秒数，需要格式化
      const displayValue = typeof value === 'number' ? formatDuration(value) : value;
      return <span className="font-mono text-indigo-600">{displayValue}</span>;
    }

    if (type === 'number') {
      return <span className="font-mono text-slate-700">{value}</span>;
    }

    return <span className="font-mono text-slate-700 break-all">{String(value)}</span>;
  };

  return (
    <div className="flex justify-between items-center py-3 border-b border-slate-100 last:border-0">
      <span className="text-sm text-slate-600">{label}</span>
      {renderValue()}
    </div>
  );
};

export default ConfigItem;
