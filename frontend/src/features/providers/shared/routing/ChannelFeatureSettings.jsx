import { PanelsTopLeft, Replace } from 'lucide-react';

const FEATURES = [
  { key: 'mapping', label: '模型映射', description: '配置本渠道内的模型名称替换。', Icon: Replace },

];

export default function ChannelFeatureSettings({ features, onToggle, disabled }) {
  return (
    <div className="space-y-2.5">
      <div className="flex items-center gap-2 text-sm font-medium text-slate-700">
        <PanelsTopLeft size={15} />扩展设置
      </div>
      <p className="text-xs leading-5 text-slate-400">开启后显示模型映射分页。模型路由和非流改道在账号池中配置。</p>
      <div className="divide-y divide-slate-100 rounded-xl border border-slate-200 bg-slate-50/40 px-3">
        {FEATURES.map(({ key, label, description, Icon }) => (
          <div key={key} className="flex items-center justify-between gap-3 py-3">
            <div className="flex min-w-0 items-start gap-2.5">
              <Icon size={16} className="mt-0.5 shrink-0 text-slate-400" />
              <div className="min-w-0">
                <p className="text-sm font-medium text-slate-700">{label}</p>
                <p className="mt-0.5 text-xs leading-5 text-slate-400">{description}</p>
              </div>
            </div>
            <button
              type="button"
              role="switch"
              aria-label={`显示${label}设置`}
              aria-checked={features[key]}
              onClick={() => onToggle(key)}
              disabled={disabled}
              className={`relative inline-flex h-6 w-11 shrink-0 rounded-full transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-indigo-500/30 disabled:opacity-50 ${features[key] ? 'bg-indigo-600' : 'bg-slate-200'}`}
            >
              <span className={`absolute left-0.5 top-0.5 h-5 w-5 rounded-full bg-white shadow-sm transition-transform ${features[key] ? 'translate-x-5' : ''}`} />
            </button>
          </div>
        ))}
      </div>
    </div>
  );
}
