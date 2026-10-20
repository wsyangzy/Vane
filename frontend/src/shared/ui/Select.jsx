import { CustomSelect } from './CustomSelect.jsx';

export const Select = ({
  label,
  options = [],
  className = '',
  value,
  onChange,
  disabled = false,
  placeholder = '请选择',
  size = 'md',
  fullWidth = true,
  minWidth,
  ...props
}) => {
  const handleChange = (nextValue) => {
    onChange?.({
      target: {
        value: nextValue,
        name: props.name
      }
    });
  };

  return (
    <div className="flex flex-col">
      {label && (
        <label className="text-sm font-medium text-slate-700 mb-1.5">{label}</label>
      )}
      <CustomSelect
        options={options}
        value={value}
        onChange={handleChange}
        disabled={disabled}
        placeholder={placeholder}
        size={size}
        fullWidth={fullWidth}
        minWidth={minWidth}
        className={className}
      />
    </div>
  );
};
