// ============================================
// EndpointsSection - 端点配置展示组件
// 专门用于展示endpoints数组配置
// 2025-12-01
// ============================================

import { Server, ExternalLink } from 'lucide-react';
import ConfigSection from './ConfigSection.jsx';

const EndpointsSection = ({ endpoints }) => {
  // 格式化纳秒为可读时间格式
  const formatDuration = (ns) => {
    if (typeof ns === 'string') return ns; // 已经是字符串格式
    const seconds = ns / 1000000000;
    if (seconds < 1) return `${Math.round(ns / 1000000)}ms`;
    if (seconds < 60) return `${Math.round(seconds)}s`;
    if (seconds < 3600) return `${Math.round(seconds / 60)}m`;
    return `${Math.round(seconds / 3600)}h`;
  };

  if (!endpoints || !Array.isArray(endpoints) || endpoints.length === 0) {
    return null;
  }

  return (
    <ConfigSection title={`端点配置 (${endpoints.length})`} icon={Server}>
      <div className="space-y-4">
        {endpoints.map((endpoint, index) => (
          <div
            key={index}
            className="p-4 bg-slate-50 rounded-xl border border-slate-200 hover:border-indigo-300 transition-colors"
          >
            {/* 端点名称和URL */}
            <div className="flex items-start justify-between mb-3">
              <div>
                <div className="font-semibold text-slate-900 flex items-center gap-2">
                  {endpoint.Name}
                  {endpoint.IsActive && (
                    <span className="text-xs px-2 py-0.5 bg-indigo-100 text-indigo-700 rounded-full">
                      Active
                    </span>
                  )}
                </div>
                <div className="text-xs text-slate-500 flex items-center gap-1 mt-1">
                  <ExternalLink size={12} />
                  {endpoint.URL}
                </div>
              </div>
            </div>

            {/* 配置详情 */}
            <div className="grid grid-cols-2 gap-x-4 gap-y-2 text-xs">
              {endpoint.Group && (
                <div className="flex justify-between">
                  <span className="text-slate-500">Group</span>
                  <span className="font-mono text-slate-700">{endpoint.Group}</span>
                </div>
              )}
              {endpoint.GroupPriority !== undefined && (
                <div className="flex justify-between">
                  <span className="text-slate-500">Group Priority</span>
                  <span className="font-mono text-slate-700">{endpoint.GroupPriority}</span>
                </div>
              )}
              {endpoint.Priority !== undefined && (
                <div className="flex justify-between">
                  <span className="text-slate-500">Priority</span>
                  <span className="font-mono text-slate-700">{endpoint.Priority}</span>
                </div>
              )}
              {endpoint.Timeout && (
                <div className="flex justify-between">
                  <span className="text-slate-500">Timeout</span>
                  <span className="font-mono text-indigo-600">{formatDuration(endpoint.Timeout)}</span>
                </div>
              )}
              {endpoint.SupportsCountTokens !== undefined && (
                <div className="flex justify-between">
                  <span className="text-slate-500">Count Tokens</span>
                  <span className={`px-1.5 py-0.5 rounded ${
                    endpoint.SupportsCountTokens
                      ? 'bg-emerald-100 text-emerald-700'
                      : 'bg-slate-200 text-slate-600'
                  }`}>
                    {endpoint.SupportsCountTokens ? '支持' : '不支持'}
                  </span>
                </div>
              )}
              {endpoint.Token && (
                <div className="flex justify-between col-span-2">
                  <span className="text-slate-500">Token</span>
                  <span className="font-mono text-slate-700 text-xs">
                    {endpoint.Token.substring(0, 15)}...{endpoint.Token.substring(endpoint.Token.length - 8)}
                  </span>
                </div>
              )}
              {endpoint.Tokens && Array.isArray(endpoint.Tokens) && (
                <div className="flex justify-between col-span-2">
                  <span className="text-slate-500">Tokens</span>
                  <span className="text-slate-600 bg-indigo-50 px-2 py-0.5 rounded">
                    {endpoint.Tokens.length} keys configured
                  </span>
                </div>
              )}
            </div>

            {/* Headers（如果有） */}
            {endpoint.Headers && Object.keys(endpoint.Headers).length > 0 && (
              <div className="mt-3 pt-3 border-t border-slate-200">
                <div className="text-xs text-slate-500 mb-2">Custom Headers:</div>
                <div className="space-y-1">
                  {Object.entries(endpoint.Headers).map(([key, value]) => (
                    <div key={key} className="flex justify-between text-xs">
                      <span className="text-slate-600">{key}</span>
                      <span className="font-mono text-slate-500">{value}</span>
                    </div>
                  ))}
                </div>
              </div>
            )}
          </div>
        ))}
      </div>
    </ConfigSection>
  );
};

export default EndpointsSection;
