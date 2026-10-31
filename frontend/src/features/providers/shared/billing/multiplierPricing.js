export const normalizePricingTier = (value) => {
  const tier = String(value ?? '').trim().toLowerCase();
  if (!tier || ['default', 'standard', 'standard_only'].includes(tier)) return '';
  return tier === 'priority' ? 'fast' : tier;
};

export const filterPricingsByTier = (records, serviceTier) => {
  if (serviceTier == null || String(serviceTier).trim() === '') return records;
  return records.filter((item) => normalizePricingTier(item.serviceTier) === normalizePricingTier(serviceTier));
};

export const getCacheWritePolicyLabel = (cacheWriteTTLPricing) => cacheWriteTTLPricing ? '30m / 12h 共用' : '30m';

// Long context cache prices fall back to ordinary prices, just as billing does.
export const getMultiplierPriceReferences = (pricing, field, cacheWriteTTLPricing = false) => {
  if (!field.priceKey) return [];
  const positive = (key) => Number(pricing?.[key]) > 0 ? Number(pricing[key]) : 0;
  const longContext = field.priceKey.startsWith('longContext');
  const cacheWrite = ['cacheWritePrice30m', 'longContextCacheWritePrice'].includes(field.priceKey);
  const basePrice = positive(field.priceKey) || (longContext && field.priceKey.includes('Cache')
    ? positive(cacheWrite ? 'cacheWritePrice30m' : 'cacheReadPrice') : 0);
  const references = [{ label: cacheWrite ? '30m' : '', price: basePrice }];
  if (cacheWrite && cacheWriteTTLPricing) {
    const price12h = positive(longContext ? 'longContextCacheWritePrice12h' : 'cacheWritePrice12h');
    references.push({ label: price12h ? '12h' : '12h（回退 30m）', price: price12h || basePrice });
  }
  return references;
};
