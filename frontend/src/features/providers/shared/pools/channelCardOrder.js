export const reconcileChannelCardOrder = (keys, savedOrder) => {
  const available = new Set(keys);
  const ordered = [];
  for (const key of Array.isArray(savedOrder) ? savedOrder : []) {
    if (available.delete(key)) ordered.push(key);
  }
  return [...ordered, ...available];
};

export const moveChannelCard = (keys, source, target) => {
  const from = keys.indexOf(source);
  const to = keys.indexOf(target);
  if (from < 0 || to < 0 || from === to) return keys;
  const next = [...keys];
  next.splice(from, 1);
  next.splice(to, 0, source);
  return next;
};
