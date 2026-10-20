export const buildSettingsUpdates = (changes) => Object.entries(changes).map(([changeKey, value]) => {
  const [provider, category, ...key] = changeKey.split('.');
  return { provider, category, key: key.join('.'), value: String(value) };
});
