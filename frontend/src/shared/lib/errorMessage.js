// Wails may wrap its runtime error as JSON inside Error.message or a string.
export const getErrorMessage = (error, fallback = '操作失败') => {
  let current = error;
  for (let depth = 0; depth < 6; depth += 1) {
    if (typeof current === 'string') {
      const message = current.trim();
      if (!message) return fallback;
      try {
        const parsed = JSON.parse(message);
        if (typeof parsed === 'string' || typeof parsed?.message === 'string') {
          current = parsed;
          continue;
        }
      } catch {
        // Ordinary error text is already suitable for display.
      }
      return message;
    }
    if (typeof current?.message === 'string') {
      current = current.message;
      continue;
    }
    return fallback;
  }
  return typeof current === 'string' && current.trim() ? current.trim() : fallback;
};
