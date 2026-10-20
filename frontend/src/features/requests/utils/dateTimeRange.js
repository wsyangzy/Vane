const pad2 = (value) => String(value).padStart(2, '0');

export const parseDateTimeLocal = (value) => {
  if (!value || typeof value !== 'string') return null;

  const match = value.match(/^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2})(?::(\d{2}))?$/);
  if (!match) return null;

  const [, yearText, monthText, dayText, hourText, minuteText] = match;
  const year = Number(yearText);
  const month = Number(monthText);
  const day = Number(dayText);
  const hour = Number(hourText);
  const minute = Number(minuteText);
  const date = new Date(year, month - 1, day, hour, minute, 0);

  if (
    date.getFullYear() !== year
    || date.getMonth() !== month - 1
    || date.getDate() !== day
    || date.getHours() !== hour
    || date.getMinutes() !== minute
  ) {
    return null;
  }

  return date;
};

export const formatDateTimeLocal = (date) => (
  `${date.getFullYear()}-${pad2(date.getMonth() + 1)}-${pad2(date.getDate())}`
  + `T${pad2(date.getHours())}:${pad2(date.getMinutes())}`
);

export const formatDisplayDateTime = (date) => formatDateTimeLocal(date).replace('T', ' ');

export const formatDisplayDate = (date) => (
  `${date.getFullYear()}-${pad2(date.getMonth() + 1)}-${pad2(date.getDate())}`
);

export const isSameDay = (first, second) => Boolean(
  first
  && second
  && first.getFullYear() === second.getFullYear()
  && first.getMonth() === second.getMonth()
  && first.getDate() === second.getDate()
);

const dayTimestamp = (date) => new Date(
  date.getFullYear(),
  date.getMonth(),
  date.getDate()
).getTime();

export const isDayWithinRange = (date, start, end) => {
  if (!start || !end || start > end) return false;
  const day = dayTimestamp(date);
  return day > dayTimestamp(start) && day < dayTimestamp(end);
};

export const isValidDateTimeRange = (start, end) => Boolean(start && end && start <= end);
