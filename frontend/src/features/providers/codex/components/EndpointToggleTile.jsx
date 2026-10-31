

// ============================================================================
// 端点编辑弹窗（name + base_url）
// ============================================================================
const EndpointToggleTile = ({ label, description, icon: Icon, checked, onChange, disabled }) => {
  const toggle = () => {
    if (!disabled) onChange?.(!checked);
  };

  return (
    <button
      type="button"
      onClick={toggle}
      className={`flex w-full items-center justify-between gap-4 rounded-xl border px-4 py-3 text-left transition-colors focus:outline-none focus-visible:ring-2 focus-visible:ring-indigo-500/30 ${
        checked ? 'border-indigo-200 bg-indigo-50/40' : 'border-slate-200 bg-white hover:bg-slate-50/60'
      } ${disabled ? 'cursor-not-allowed opacity-70' : ''}`}
      role="switch"
      aria-checked={checked}
      aria-label={label}
      disabled={disabled}
    >
      <span className="flex min-w-0 items-start gap-3">
        {Icon && <span className="mt-0.5 rounded-lg bg-slate-100 p-1.5 text-slate-500"><Icon size={16} /></span>}
        <span className="min-w-0">
          <span className="block text-sm font-medium text-slate-800">{label}</span>
          {description && <span className="mt-1 block text-xs leading-5 text-slate-500">{description}</span>}
        </span>
      </span>
      <span
        aria-hidden="true"
        className={`relative inline-flex h-5 w-9 flex-shrink-0 items-center rounded-full transition-colors ${
          checked ? 'bg-indigo-500' : 'bg-slate-200'
        }`}
      >
        <span
          className={`inline-block h-4 w-4 rounded-full bg-white shadow-sm ring-1 ring-black/5 transition-transform ${
            checked ? 'translate-x-4' : 'translate-x-0.5'
          }`}
        />
      </span>
    </button>
  );
};

export { EndpointToggleTile };
