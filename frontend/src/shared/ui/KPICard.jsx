

export const KPICard = ({
  title,
  value,
  subText,
  icon: Icon,
  statusColor = 'bg-slate-100 text-slate-600',
  layout = 'stacked',
  size = 'md',
  wrapTitle = false,
  wrapValue = false,
  titleClassName = '',
  valueClassName = '',
  iconSize,
  iconContainerClassName = '',
  iconClassName = ''
}) => {
  if (layout === 'compact') {
    const isSmall = size === 'sm';
    return (
      <div className={`motion-kpi-card flex min-w-0 items-center rounded-xl border border-slate-200/60 bg-white shadow-sm ${isSmall ? 'gap-2 p-2.5' : 'gap-2.5 p-3'}`} title={[value, subText].filter(item => item !== undefined && item !== null && item !== '').join('\n')}>
        <div className={`motion-kpi-icon shrink-0 rounded-lg ${isSmall ? 'p-1' : 'p-1.5'} ${statusColor}`}>
          <Icon size={isSmall ? 14 : 16} />
        </div>
        <div className="min-w-0">
          <h3 className="text-xs font-medium leading-5 text-slate-500">{title}</h3>
          <div className={`font-semibold leading-6 tabular-nums text-slate-900 ${isSmall ? 'truncate text-base' : 'break-all text-lg'}`}>{value}</div>
        </div>
      </div>
    );
  }

  if (layout === 'side') {
    const isLarge = size === 'lg';
    const resolvedIconSize = iconSize ?? (isLarge ? 19 : 18);
    return (
      <div className={`motion-kpi-card h-full bg-white rounded-2xl border border-slate-200/60 shadow-sm flex flex-col ${isLarge ? 'p-5' : 'p-4'}`}>
        <div className={`flex items-center justify-between ${isLarge ? 'gap-3' : 'gap-2.5'}`}>
          <div className={`flex flex-shrink-0 items-center ${isLarge ? 'gap-3.5' : 'gap-3'}`}>
            <div className={`motion-kpi-icon rounded-lg flex-shrink-0 ${isLarge ? 'p-2.5' : 'p-2'} ${statusColor} ${iconContainerClassName}`}>
              <Icon size={resolvedIconSize} className={iconClassName} />
            </div>
            <h3 className={`${wrapTitle ? 'whitespace-normal break-words leading-snug' : 'whitespace-nowrap'} text-slate-500 font-semibold uppercase tracking-wider ${isLarge ? 'text-[11px]' : 'text-xs'} ${titleClassName}`}>{title}</h3>
          </div>
          <div
            className={`min-w-0 flex-1 text-right font-bold text-slate-900 cursor-default ${wrapValue ? 'whitespace-normal break-all leading-snug' : 'truncate'} ${isLarge ? 'text-[1.2rem]' : 'text-lg'} ${isLarge && wrapValue ? 'text-[1.05rem]' : ''} ${!isLarge && wrapValue ? 'text-base' : ''} ${valueClassName}`}
          >
            {value}
          </div>
        </div>
        {subText && (
          <div
            className={`mt-auto truncate whitespace-nowrap pt-2 text-slate-400 font-medium ${isLarge ? 'text-[11px]' : 'text-xs'}`}
          >
            {subText}
          </div>
        )}
      </div>
    );
  }

  return (
    <div className="motion-kpi-card bg-white p-5 rounded-2xl border border-slate-200/60 shadow-sm flex flex-col justify-between h-full group relative">
      <div className="flex justify-between items-start mb-2">
        <div className={`motion-kpi-icon p-2 rounded-lg ${statusColor}`}>
          <Icon size={20} />
        </div>
      </div>
      <div className="mt-auto">
        <h3 className="text-slate-500 text-xs font-semibold uppercase tracking-wider mb-1">{title}</h3>
        <div className="text-xl font-bold text-slate-900 truncate cursor-default">
          {value}
        </div>
      </div>
      {subText && (
        <div className="absolute left-1/2 -translate-x-1/2 -bottom-1 translate-y-full opacity-0 group-hover:opacity-100 transition-opacity duration-200 z-10 pointer-events-none">
          <div className="px-3 py-1.5 bg-slate-800 text-white text-xs font-medium rounded-lg shadow-lg text-center w-max max-w-[280px]">
            {subText}
          </div>
        </div>
      )}
    </div>
  );
};
