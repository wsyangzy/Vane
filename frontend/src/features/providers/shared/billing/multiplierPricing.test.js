import assert from 'node:assert/strict';
import test from 'node:test';
import { filterPricingsByTier, getMultiplierPriceReferences } from './multiplierPricing.js';

test('traditional pricing references contain only Standard and rule references match their tier', () => {
  const records = ['', 'default', 'standard', 'priority', 'flex', 'ultrafast'].map((serviceTier) => ({ serviceTier }));
  assert.deepEqual(filterPricingsByTier(records, 'standard'), records.slice(0, 3));
  assert.deepEqual(filterPricingsByTier(records, 'fast'), [records[3]]);
  assert.equal(filterPricingsByTier(records, ''), records);
});

test('cache write reference follows the configured TTL pricing policy', () => {
  const pricing = { cacheWritePrice30m: 2, cacheWritePrice12h: 5 };
  const field = { priceKey: 'cacheWritePrice30m' };
  assert.deepEqual(getMultiplierPriceReferences(pricing, field), [{ label: '30m', price: 2 }]);
  assert.deepEqual(getMultiplierPriceReferences(pricing, field, true), [
    { label: '30m', price: 2 }, { label: '12h', price: 5 }
  ]);
  assert.deepEqual(getMultiplierPriceReferences({ cacheWritePrice30m: 2 }, field, true), [
    { label: '30m', price: 2 }, { label: '12h（回退 30m）', price: 2 }
  ]);
});

test('Long context cache references follow billing fallbacks instead of ordinary 12h prices', () => {
  const pricing = { cacheWritePrice30m: 2, cacheWritePrice12h: 5, cacheReadPrice: 0.2 };
  const field = { priceKey: 'longContextCacheWritePrice' };
  assert.deepEqual(getMultiplierPriceReferences(pricing, field, true), [
    { label: '30m', price: 2 }, { label: '12h（回退 30m）', price: 2 }
  ]);
  assert.deepEqual(getMultiplierPriceReferences({ ...pricing, longContextCacheWritePrice: 4 }, field, true), [
    { label: '30m', price: 4 }, { label: '12h（回退 30m）', price: 4 }
  ]);
  assert.deepEqual(getMultiplierPriceReferences({ ...pricing, longContextCacheWritePrice12h: 8 }, field, true), [
    { label: '30m', price: 2 }, { label: '12h', price: 8 }
  ]);
  assert.deepEqual(getMultiplierPriceReferences(pricing, { priceKey: 'longContextCacheReadPrice' }), [{ label: '', price: 0.2 }]);
  assert.deepEqual(getMultiplierPriceReferences(pricing, {}), []);
});
