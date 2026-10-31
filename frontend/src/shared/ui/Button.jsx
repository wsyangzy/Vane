import { Loader2 } from 'lucide-react';
import Tooltip from './Tooltip.jsx';

export const Button = ({
  children,
  variant = 'primary',
  size = 'md',
  icon: Icon,
  loading = false,
  disabled = false,
  className = '',
  title,
  tooltip,
  'aria-label': ariaLabel,
  ...props
}) => {
  const variants = {
    primary: 'bg-indigo-600 text-white hover:bg-indigo-700 shadow-sm',
    secondary: 'bg-slate-100 text-slate-600 hover:bg-slate-200',
    success: 'bg-emerald-600 text-white hover:bg-emerald-700',
    danger: 'bg-rose-600 text-white hover:bg-rose-700',
    dangerSoft: 'bg-rose-50 text-rose-700 hover:bg-rose-100 border border-rose-200',
    ghost: 'bg-transparent text-slate-600 hover:bg-slate-100'
  };

  const sizes = {
    sm: 'px-3 py-1.5 text-xs',
    md: 'px-4 py-2 text-sm',
    lg: 'px-6 py-3 text-base'
  };

  const tooltipContent = tooltip ?? title;
  const buttonClassName = `
        motion-button inline-flex items-center justify-center font-medium rounded-lg
        ${variants[variant]} ${sizes[size]}
        ${disabled || loading ? 'opacity-50 cursor-not-allowed' : ''}
        ${className}
      `;

  const button = (
    <button
      disabled={disabled || loading}
      className={buttonClassName}
      aria-label={ariaLabel || (typeof tooltipContent === 'string' ? tooltipContent : undefined)}
      {...props}
    >
      {loading ? (
        <Loader2 size={16} className="animate-spin mr-2" />
      ) : Icon && (
        <Icon size={16} className="mr-2" />
      )}
      {children}
    </button>
  );

  if (!tooltipContent) return button;

  return (
    <Tooltip content={tooltipContent} className={className.includes('w-full') ? 'w-full' : ''}>
      {button}
    </Tooltip>
  );
};
