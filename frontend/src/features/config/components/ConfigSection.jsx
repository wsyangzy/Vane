// ============================================
// ConfigSection - 配置区块组件
// 2025-12-01
// ============================================

const ConfigSection = ({ title, icon: Icon, children }) => (
  <div className="bg-white rounded-2xl border border-slate-200/60 shadow-sm overflow-hidden">
    <div className="px-6 py-4 bg-slate-50/50 border-b border-slate-100 flex items-center">
      {Icon && <Icon size={18} className="text-indigo-600 mr-3" />}
      <h3 className="font-semibold text-slate-900">{title}</h3>
    </div>
    <div className="p-6">
      {children}
    </div>
  </div>
);

export default ConfigSection;
