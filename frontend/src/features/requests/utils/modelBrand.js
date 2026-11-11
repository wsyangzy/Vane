// Model brands describe the model itself; provider describes the client protocol.
export function getModelBrand(model, provider = 'claude') {
  const name = String(model || '').trim().toLowerCase();
  if (/(?:^|[^a-z0-9])deepseek(?:[^a-z]|$)/.test(name)) return 'deepseek';
  if (/(?:^|[^a-z0-9])glm(?:[^a-z]|$)/.test(name)) return 'glm';
  if (/(?:^|[^a-z0-9])(?:kimi|moonshot)(?:[^a-z]|$)/.test(name)) return 'kimi';
  if (/(?:^|[^a-z0-9])qwen(?:[^a-z]|$)/.test(name)) return 'qwen';
  if (/(?:claude|sonnet|haiku|opus)/.test(name)) return 'claude';
  if (/(?:^|[^a-z0-9])(?:gpt|chatgpt|codex|o[1-9])(?:[^a-z]|$)/.test(name)) return 'openai';
  return provider === 'codex' ? 'codex' : 'claude';
}
