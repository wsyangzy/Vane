// Compact and comfortable widths include padding. They are allocation weights,
// not minimum widths: selected columns must always fit inside the table region.
const widths = {
  requestId: [82, 104],
  timestamp: [132, 156],
  provider: [48, 60],
  requestType: [40, 62],
  status: [64, 80],
  // Include the brand icon and two mapping/routing indicators before ellipsis.
  model: [152, 208],
  reasoningEffort: [54, 70],
  serviceTier: [66, 84],
  channel: [92, 132],
  endpoint: [112, 156],
  duration: [108, 148],
  inputTokens: [44, 64],
  outputTokens: [44, 64],
  cacheCreationTokens: [44, 60],
  cacheReadTokens: [44, 60],
  cost: [60, 78],
};

// A complete page fills the available body. A short last page keeps that same
// row density, while smaller windows and larger page sizes scroll naturally.
export function requestRowHeight(regionHeight, headerHeight, pageSize) {
  // Leave one pixel for collapsed borders and fractional layout rounding so a
  // complete page does not create a tiny, unnecessary scroll range.
  return Math.max(40, (Math.max(0, Number(regionHeight) || 0) - Math.max(0, Number(headerHeight) || 0) - 1) / Math.max(1, Number(pageSize) || 10));
}

export function requestColumnLayout(columns, availableWidth, showDate = true) {
  const budget = Number.isFinite(availableWidth) && availableWidth > 0 ? availableWidth : 900;
  if (!columns.length) return { columns: [], tableWidth: 0 };
  const columnWidths = (id) => id === 'timestamp' && !showDate ? [68, 80] : widths[id] || [64, 100];
  const compact = columns.reduce((sum, column) => sum + columnWidths(column.id)[0], 0);
  const flexible = columns.reduce((sum, column) => { const [min, ideal] = columnWidths(column.id); return sum + ideal - min; }, 0);
  const scale = Math.max(0, budget - compact) / flexible;
  return {
    columns: columns.map((column) => {
      const [base, ideal] = columnWidths(column.id);
      return { ...column, layoutWidth: budget < compact ? base * budget / compact : base + (ideal - base) * scale };
    }),
    tableWidth: budget,
  };
}
