

export const StatDetailItem = ({ label, value, unit, valueColor }) => (
  <div className="bg-slate-50 rounded-xl p-4 flex flex-col items-center justify-center border border-slate-100 hover:border-slate-200 transition-colors group">
    <div className={`text-2xl font-bold mb-1 ${valueColor || 'text-slate-900'} group-hover:scale-105 transition-transform`}>
      {value}<span className="text-sm font-medium text-slate-500 ml-0.5">{unit}</span>
    </div>
    <div className="text-xs text-slate-400 font-medium">{label}</div>
  </div>
);
