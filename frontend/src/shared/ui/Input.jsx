

export const Input = ({
  label,
  error,
  className = '',
  ...props
}) => (
  <div className="flex flex-col">
    {label && (
      <label className="text-sm font-medium text-slate-700 mb-1.5">{label}</label>
    )}
    <input
      className={`
        w-full px-3 py-2 border rounded-lg text-sm
        focus:outline-none focus:ring-2 focus:ring-indigo-500 focus:border-indigo-500
        ${error ? 'border-rose-300 focus:ring-rose-500 focus:border-rose-500' : 'border-slate-200'}
        ${className}
      `}
      {...props}
    />
    {error && (
      <span className="text-xs text-rose-500 mt-1">{error}</span>
    )}
  </div>
);
