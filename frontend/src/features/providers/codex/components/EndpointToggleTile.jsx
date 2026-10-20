

// ============================================================================
// 端点编辑弹窗（name + base_url）
// ============================================================================
const EndpointToggleTile = ({ label, checked, onChange, disabled }) => {
  const toggle = () => {
    if (!disabled) onChange?.(!checked);
  };

  return (
    <div
      onClick={toggle}
      className={`flex cursor-pointer items-center justify-between rounded-xl border p-3 transition-colors ${
        checked ? 'border-emerald-200 bg-white' : 'border-slate-200 bg-white hover:bg-slate-50/60'
      } ${disabled ? 'cursor-not-allowed opacity-70' : ''}`}
      role="button"
      tabIndex={disabled ? -1 : 0}
      onKeyDown={(event) => {
        if (event.key === 'Enter' || event.key === ' ') {
          event.preventDefault();
          toggle();
        }
      }}
    >
      <span className="min-w-0 truncate text-xs font-semibold text-slate-800">{label}</span>
      <button
        type="button"
        role="switch"
        aria-checked={checked}
        aria-label={label}
        disabled={disabled}
        onClick={(event) => {
          event.stopPropagation();
          toggle();
        }}
        className={`relative inline-flex h-5 w-9 flex-shrink-0 items-center rounded-full border transition-colors focus:outline-none focus:ring-2 focus:ring-indigo-500/30 ${
          checked ? 'border-emerald-500 bg-emerald-500' : 'border-slate-200 bg-slate-200'
        } ${disabled ? 'cursor-not-allowed' : 'cursor-pointer'}`}
      >
        <span
          className={`inline-block h-4 w-4 rounded-full bg-white shadow-sm ring-1 ring-black/5 transition-transform ${
            checked ? 'translate-x-4' : 'translate-x-0.5'
          }`}
        />
      </button>
    </div>
  );
};

export { EndpointToggleTile };
