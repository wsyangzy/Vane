import { Repeat, Slash } from 'lucide-react';

const RepeatOffIcon = ({ size = 12 }) => (
  <span className="relative inline-flex flex-shrink-0" style={{ width: size, height: size }}>
    <Repeat size={size} />
    <Slash size={size} strokeWidth={2.5} className="absolute inset-0" />
  </span>
);



export { RepeatOffIcon };
