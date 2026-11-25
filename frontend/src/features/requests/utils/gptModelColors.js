const modelColors = {
  '6-astra': 'bg-[#FFF1F2] text-[#9F1239] border-[#FDA4AF]',
  '6-sol': 'bg-orange-50 text-orange-800 border-orange-300',
  '6-luna': 'bg-indigo-50 text-indigo-900 border-indigo-300',
  '6.1-sol': 'bg-orange-50 text-orange-800 border-orange-300',
  '6.1-terra': 'bg-teal-50 text-teal-800 border-teal-300',
  '6.2-astra': 'bg-rose-100 text-rose-900 border-rose-300',
  '6.2-sol': 'bg-amber-100 text-amber-900 border-amber-300',
};

// Keep the palette order and length stable so new models do not recolor existing labels.
const autoColors = [
  'bg-sky-50 text-sky-900 border-sky-300',
  'bg-teal-50 text-teal-900 border-teal-300',
  'bg-amber-50 text-amber-900 border-amber-300',
  'bg-indigo-50 text-indigo-900 border-indigo-300',
  'bg-rose-50 text-rose-900 border-rose-300',
  'bg-lime-50 text-lime-900 border-lime-300',
  'bg-stone-50 text-stone-900 border-stone-300',
];

export function getModernGptColor(lowerName) {
  const match = lowerName.match(/\bgpt[-.]?(\d+)(?:[.-](\d+))?((?:[.-][a-z0-9]+)*)\b/);
  if (!match || Number(match[1]) < 6) return null;

  const [, major, minor, suffix] = match;
  const version = minor ? `${major}.${minor}` : major;
  const variants = suffix.slice(1).split(/[.-]/);
  const preset = modelColors[`${version}-${variants[0]}`];
  if (preset) return preset;

  const key = [version, ...variants].join('-');
  let hash = 2166136261;
  for (let index = 0; index < key.length; index++) {
    hash = Math.imul(hash ^ key.charCodeAt(index), 16777619);
  }
  return autoColors[(hash >>> 0) % autoColors.length];
}
