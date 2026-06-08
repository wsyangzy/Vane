const NUMBER_PATTERN = '[+-]?(?:\\d+(?:\\.\\d*)?|\\.\\d+)';
const FRACTION_PATTERN = new RegExp(`^(${NUMBER_PATTERN})\\s*/\\s*(${NUMBER_PATTERN})$`);
const NON_TERMINATING_TOLERANCE = 0.000001;

const getGreatestCommonDivisor = (a, b) => {
  let x = Math.abs(a);
  let y = Math.abs(b);
  while (y) {
    const next = x % y;
    x = y;
    y = next;
  }
  return x || 1;
};

const hasOnlyTerminatingDecimalFactors = (denominator) => {
  let value = Math.abs(denominator);
  while (value % 2 === 0) value /= 2;
  while (value % 5 === 0) value /= 5;
  return value === 1;
};

const getApproximateFraction = (value, maxDenominator = 100) => {
  const parsed = Number(value);
  if (!Number.isFinite(parsed)) return null;

  const sign = parsed < 0 ? -1 : 1;
  const absoluteValue = Math.abs(parsed);
  let best = null;

  for (let denominator = 1; denominator <= maxDenominator; denominator += 1) {
    const numerator = Math.round(absoluteValue * denominator);
    const approximation = numerator / denominator;
    const error = Math.abs(absoluteValue - approximation);

    if (!best || error < best.error) {
      const divisor = getGreatestCommonDivisor(numerator, denominator);
      best = {
        numerator: sign * (numerator / divisor),
        denominator: denominator / divisor,
        error
      };
    }
  }

  if (!best || best.denominator === 1 || best.error > NON_TERMINATING_TOLERANCE) {
    return null;
  }
  return best;
};

export const parseMultiplierExpression = (value, fallback = 1.0) => {
  const text = String(value ?? '').trim();
  if (!text) return fallback;

  const fractionMatch = text.match(FRACTION_PATTERN);
  if (fractionMatch) {
    const numerator = Number(fractionMatch[1]);
    const denominator = Number(fractionMatch[2]);
    if (!Number.isFinite(numerator) || !Number.isFinite(denominator) || denominator === 0) {
      return Number.NaN;
    }
    return numerator / denominator;
  }

  const parsed = Number(text);
  return Number.isFinite(parsed) ? parsed : Number.NaN;
};

export const isValidMultiplierExpression = (value) => {
  const parsed = parseMultiplierExpression(value, Number.NaN);
  return Number.isFinite(parsed) && parsed >= 0;
};

export const formatMultiplierValue = (value, maxFractionDigits = 3) => {
  const parsed = Number(value);
  if (!Number.isFinite(parsed)) return '1';
  return parsed.toFixed(maxFractionDigits).replace(/\.?0+$/, '');
};

export const formatMultiplierExpression = (value, maxFractionDigits = 3) => {
  const fraction = getApproximateFraction(value);
  if (fraction && !hasOnlyTerminatingDecimalFactors(fraction.denominator)) {
    return `(${fraction.numerator} / ${fraction.denominator})`;
  }
  return formatMultiplierValue(value, maxFractionDigits);
};

export const formatMultiplierDisplayValue = (value, maxFractionDigits = 3) => {
  const expression = formatMultiplierExpression(value, maxFractionDigits);
  return expression.startsWith('(') ? expression : `${expression}x`;
};

export const formatMultiplierInputValue = (value) => {
  if (value === '' || value === null || value === undefined) return '';
  if (typeof value === 'string' && value.includes('/')) return value;

  const parsed = Number(value);
  if (!Number.isFinite(parsed)) return String(value);
  const fraction = getApproximateFraction(parsed);
  if (fraction && !hasOnlyTerminatingDecimalFactors(fraction.denominator)) {
    return `${fraction.numerator}/${fraction.denominator}`;
  }
  return formatMultiplierValue(parsed, 6);
};
