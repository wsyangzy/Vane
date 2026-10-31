const dateKey = (date) => `${date.getFullYear()}-${String(date.getMonth() + 1).padStart(2, '0')}-${String(date.getDate()).padStart(2, '0')}`;

export function heatmapRange(now = new Date()) {
  const today = new Date(now.getFullYear(), now.getMonth(), now.getDate());
  const start = new Date(today);
  start.setDate(start.getDate() - ((start.getDay() + 6) % 7) - 52 * 7);
  return { start, today, startDate: `${dateKey(start)}T00:00:00`, endDate: `${dateKey(today)}T23:59:59.999` };
}

export function buildUsageCalendar(items, metric = 'tokens', now = new Date()) {
  // The backend's calendar can be in a configured timezone different from the browser.
  const lastDay = items.at(-1)?.date?.slice(0, 10);
  const anchor = /^\d{4}-\d{2}-\d{2}$/.test(lastDay || '') ? new Date(`${lastDay}T12:00:00`) : now;
  const { start, today } = heatmapRange(anchor);
  const byDate = new Map(items.map((item) => [item.date.slice(0, 10), item]));
  const days = Array.from({ length: 371 }, (_, index) => {
    const date = new Date(start);
    date.setDate(date.getDate() + index);
    const key = dateKey(date);
    const data = byDate.get(key);
    return { date: key, data, future: date > today, week: Math.floor(index / 7), weekday: index % 7, value: Number(metric === 'requests' ? data?.requestCount || 0 : data?.total || 0) };
  });
  const positive = days.filter((day) => !day.future && day.value > 0).map((day) => day.value).sort((a, b) => a - b);
  // Quantiles keep quieter days visible when a few sessions dominate the year.
  const thresholds = [0.25, 0.5, 0.75].map((quantile) => positive[Math.min(positive.length - 1, Math.floor(positive.length * quantile))] || 0);
  for (const day of days) day.level = day.value <= 0 || day.future ? 0 : 1 + thresholds.filter((threshold) => day.value > threshold).length;
  const months = [];
  let previousMonth = -1;
  for (let week = 0; week < 53; week += 1) {
    const monday = new Date(`${days[week * 7].date}T12:00:00`);
    if (monday.getMonth() === previousMonth) continue;
    previousMonth = monday.getMonth();
    if ((week === 0 && monday.getDate() > 14) || week === 52 || (months.length && week - months.at(-1).week < 3)) continue;
    months.push({ week, label: `${monday.getMonth() + 1}月` });
  }
  return { days, months, total: days.reduce((sum, day) => sum + (day.future ? 0 : day.value), 0), peak: positive.at(-1) || 0, activeDays: days.filter((day) => !day.future && (Number(day.data?.requestCount || 0) > 0 || Number(day.data?.total || 0) > 0)).length };
}
