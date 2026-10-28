// Compact and comfortable widths include padding. They are allocation weights,
// not minimum widths: selected columns must always fit inside the table region.
const widths = { requestId: [82, 112], timestamp: [60, 142], provider: [46, 64], requestType: [40, 62], status: [54, 78], model: [114, 190], reasoningEffort: [42, 62], channel: [60, 108], endpoint: [60, 108], duration: [86, 132], inputTokens: [38, 62], outputTokens: [38, 62], cacheCreationTokens: [40, 70], cacheReadTokens: [40, 70], cost: [60, 78] };

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
