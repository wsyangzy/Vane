// Compact and comfortable widths include padding. They are allocation weights,
// not minimum widths: selected columns must always fit inside the table region.
const widths = {
  requestId: [82, 112],
  timestamp: [60, 142],
  provider: [50, 72],
  requestType: [40, 62],
  status: [64, 90],
  model: [100, 148],
  reasoningEffort: [62, 82],
  serviceTier: [70, 96],
  channel: [72, 112],
  endpoint: [72, 112],
  duration: [108, 156],
  inputTokens: [38, 62],
  outputTokens: [38, 62],
  cacheCreationTokens: [40, 70],
  cacheReadTokens: [40, 70],
  cost: [60, 78],
};

// A complete page fills the available body. A short last page keeps that same
// row density, while smaller windows and larger page sizes scroll naturally.
export function requestRowHeight(regionHeight, headerHeight, pageSize) {
  // Leave one pixel for collapsed borders and fractional layout rounding so a
  // complete page does not create a tiny, unnecessary scroll range.
  return Math.max(40, (Math.max(0, Number(regionHeight) || 0) - Math.max(0, Number(headerHeight) || 0) - 1) / Math.max(1, Number(pageSize) || 10));
}

export function requestColumnLayout(columns, availableWidth) {
  const budget = Number.isFinite(availableWidth) && availableWidth > 0 ? availableWidth : 900;
  if (!columns.length) return { columns: [], tableWidth: 0 };
  const compact = columns.reduce((sum, column) => sum + (widths[column.id]?.[0] || 64), 0);
  const flexible = columns.reduce((sum, column) => { const [min, ideal] = widths[column.id] || [64, 100]; return sum + ideal - min; }, 0);
  const scale = Math.max(0, budget - compact) / flexible;
  return {
    columns: columns.map((column) => {
      const [base, ideal] = widths[column.id] || [64, 100];
      return { ...column, layoutWidth: budget < compact ? base * budget / compact : base + (ideal - base) * scale };
    }),
    tableWidth: budget,
  };
}
