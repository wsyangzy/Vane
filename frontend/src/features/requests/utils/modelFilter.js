export const normalizeModelSelection = (value) => [...new Set(
  (Array.isArray(value) ? value : [value])
    .filter(item => typeof item === 'string')
    .map(item => item.trim())
    .filter(Boolean)
)];

// Pricing records are distinct by provider and tier; this filter only matches model names.
export const requestModelOptions = (models = [], selected = []) => normalizeModelSelection([
  ...models.map(model => typeof model === 'string' ? model : model?.model_name || model?.modelName || model?.name || ''),
  ...selected
]).sort((a, b) => a.localeCompare(b));
