import { useState } from 'react';

// Channel settings own model mapping; request rules are edited in dispatch profiles.
export function useChannelFeatureTabs({ provider, channelId, modelMapping, tabs }) {
  const storageKey = `vane:channel-settings-tabs:${provider}:${channelId || 'new'}`;
  const [features, setFeatures] = useState(() => {
    let saved;
    try { saved = JSON.parse(localStorage.getItem(storageKey)); } catch { /* Keep the configuration default. */ }
    return { mapping: typeof saved?.mapping === 'boolean' ? saved.mapping : Object.keys(modelMapping || {}).length > 0 };
  });
  const toggleFeature = () => {
    const next = { mapping: !features.mapping };
    setFeatures(next);
    try { localStorage.setItem(storageKey, JSON.stringify(next)); } catch { /* Keep in memory. */ }
  };
  return { features, toggleFeature, visibleTabs: tabs.filter((tab) => tab.key === 'basic' || tab.key === 'mapping' && features.mapping) };
}
