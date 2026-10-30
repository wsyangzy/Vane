

const getSegmentedTabsSliderStyle = (activeIndex, tabCount) => {
  const safeCount = Math.max(tabCount, 1);
  const safeIndex = Math.min(Math.max(activeIndex, 0), safeCount - 1);
  const gapTotalRem = Math.max(safeCount - 1, 0) * 0.25;
  const tabWidth = `calc((100% - 0.5rem - ${gapTotalRem}rem) / ${safeCount})`;

  return {
    width: tabWidth,
    left: '0.25rem',
    transform: `translateX(calc(${safeIndex * 100}% + ${safeIndex * 0.25}rem))`
  };
};

export const SegmentedTabs = ({
  tabs = [],
  activeTab,
  onChange,
  disabled = false,
  className = 'grid-cols-2',
  buttonPaddingClass = 'px-3'
}) => {
  const activeIndex = tabs.findIndex((tab) => tab.key === activeTab);

  return (
    <div className={`relative grid gap-1 rounded-xl bg-slate-100 p-1 ${className}`}>
      {activeIndex >= 0 && <span
        aria-hidden="true"
        className="motion-tabs-slider pointer-events-none absolute inset-y-1 rounded-lg bg-white shadow-sm ring-1 ring-indigo-100"
        style={getSegmentedTabsSliderStyle(activeIndex, tabs.length)}
      />}
      {tabs.map((tab) => {
        const active = activeTab === tab.key;
        return (
          <button
            key={tab.key}
            type="button"
            onClick={() => onChange?.(tab.key)}
            className={`
              relative z-10 inline-flex h-8 items-center justify-center rounded-lg ${buttonPaddingClass}
              motion-tabs-label text-xs font-semibold
              ${active ? 'text-indigo-700' : 'text-slate-500 hover:text-slate-700'}
            `}
            disabled={disabled}
            aria-pressed={active}
          >
            {tab.label}
          </button>
        );
      })}
    </div>
  );
};
