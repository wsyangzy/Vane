// Content updates keep column assignments so a growing card only moves the
// cards below it. Rebalance when the container width or resource list changes.
export function restackHealthCards(heights, previousPositions, gap = 12) {
  const bottoms = new Map();
  const positions = heights.map((height, index) => {
    const column = previousPositions[index].column;
    const top = bottoms.has(column) ? bottoms.get(column) + gap : 0;
    bottoms.set(column, top + Math.max(0, Number(height) || 0));
    return { column, top };
  });
  return { positions, height: Math.max(0, ...bottoms.values()) };
}

// Place larger cards first so several short cards can balance a tall card.
// Keep the configured channel order within each resulting column.
export function balanceHealthCards(heights, columnCount, gap = 12) {
  if (!heights.length) return { positions: [], height: 0 };
  const count = Math.max(1, Math.min(heights.length, Math.floor(columnCount) || 1));
  const columns = Array.from({ length: count }, () => ({ indices: [], height: 0 }));
  const sizes = heights.map((height) => Math.max(0, Number(height) || 0));
  const order = sizes.map((_, index) => index).sort((a, b) => sizes[b] - sizes[a] || a - b);
  for (const index of order) {
    const column = columns.reduce((shortest, current) => current.height < shortest.height ? current : shortest);
    column.height += sizes[index] + (column.indices.length ? gap : 0);
    column.indices.push(index);
  }
  // Greedy placement can leave one column a whole card taller. Refine it with
  // moves and swaps, keeping every column occupied and preferring a smaller
  // bottom spread before the overall variance. Each accepted step improves it.
  const score = (values) => [Math.max(...values) - Math.min(...values), values.reduce((sum, value) => sum + value * value, 0)];
  const better = (next, previous) => next[0] < previous[0] - 0.01 || Math.abs(next[0] - previous[0]) < 0.01 && next[1] < previous[1] - 0.01;
  for (let pass = 0; pass < sizes.length; pass++) {
    const loads = columns.map((column) => column.height);
    let bestScore = score(loads);
    let improvement = null;
    const consider = (from, to, index, otherIndex = null) => {
      const change = otherIndex == null ? sizes[index] + gap : sizes[index] - sizes[otherIndex];
      const nextLoads = loads.map((load, column) => load + (column === from ? -change : column === to ? change : 0));
      const nextScore = score(nextLoads);
      if (better(nextScore, bestScore)) {
        bestScore = nextScore;
        improvement = { from, to, index, otherIndex, nextLoads };
      }
    };
    columns.forEach((column, from) => {
      columns.forEach((target, to) => {
        if (from === to) return;
        for (const index of column.indices) {
          if (column.indices.length > 1) consider(from, to, index);
          if (from < to) for (const otherIndex of target.indices) consider(from, to, index, otherIndex);
        }
      });
    });
    if (!improvement) break;
    const { from, to, index, otherIndex, nextLoads } = improvement;
    columns[from].indices = columns[from].indices.filter((value) => value !== index);
    columns[to].indices.push(index);
    if (otherIndex != null) {
      columns[to].indices = columns[to].indices.filter((value) => value !== otherIndex);
      columns[from].indices.push(otherIndex);
    }
    columns.forEach((column, index) => { column.height = nextLoads[index]; });
  }
  // A small resource list can still trap moves/swaps in a local optimum. Search
  // alternative combinations with a fixed node budget so resizing stays cheap.
  if (count > 1 && count < sizes.length && sizes.length <= 24) {
    let bestScore = score(columns.map((column) => column.height));
    const total = sizes.reduce((sum, height) => sum + height + gap, 0);
    const trial = Array.from({ length: count }, () => ({ indices: [], height: 0 }));
    let nodes = 0;
    const search = (position) => {
      if (++nodes > 20000 || bestScore[0] < 0.01) return;
      if (position === order.length) {
        if (trial.some((column) => !column.indices.length)) return;
        const nextScore = score(trial.map((column) => column.height - gap));
        if (better(nextScore, bestScore)) {
          bestScore = nextScore;
          trial.forEach((column, index) => { columns[index] = { indices: [...column.indices], height: column.height - gap }; });
        }
        return;
      }
      const index = order[position];
      const visited = new Set();
      for (const column of trial) {
        if (nodes >= 20000) break;
        if (visited.has(column.height)) continue;
        visited.add(column.height);
        const nextHeight = column.height + sizes[index] + gap;
        // Any column above this bound makes the current bottom spread worse,
        // even if all remaining cards could fill the other columns perfectly.
        if (nextHeight > (total + (count - 1) * bestScore[0]) / count + 0.01) continue;
        const previousHeight = column.height;
        column.height = nextHeight;
        column.indices.push(index);
        search(position + 1);
        column.indices.pop();
        column.height = previousHeight;
      }
    };
    search(0);
  }
  columns.forEach((column) => column.indices.sort((a, b) => a - b));
  columns.sort((a, b) => a.indices[0] - b.indices[0]);
  const positions = Array(heights.length);
  columns.forEach((column, columnIndex) => {
    let top = 0;
    for (const index of column.indices) {
      positions[index] = { column: columnIndex, top };
      top += sizes[index] + gap;
    }
  });
  return { positions, height: Math.max(...columns.map((column) => column.height)) };
}
