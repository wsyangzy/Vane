import { WailsApp, initWails } from './connection.js';

// ============================================
// 模型定价管理 API (SQLite)
// ============================================

const mapModelPricingRecord = (record = {}) => ({
  id: record.id,
  modelName: record.model_name,
  provider: record.provider || 'claude',
  serviceTier: record.service_tier || '',
  displayName: record.display_name,
  description: record.description,
  inputPrice: record.input_price,
  outputPrice: record.output_price,
  cacheCreationPrice5m: record.cache_creation_price_5m,
  cacheCreationPrice1h: record.cache_creation_price_1h,
  cacheReadPrice: record.cache_read_price,
  cacheWritePrice30m: record.cache_write_price_30m ?? record.cache_creation_price_5m ?? 0,
  cacheWritePrice12h: record.cache_write_price_12h ?? 0,
  longContextCacheWritePrice12h: record.long_context_cache_write_price_12h ?? 0,
  longContextThreshold: record.long_context_threshold ?? 0,
  longContextInputPrice: record.long_context_input_price ?? 0,
  longContextCacheReadPrice: record.long_context_cache_read_price ?? 0,
  longContextCacheWritePrice: record.long_context_cache_write_price ?? 0,
  longContextOutputPrice: record.long_context_output_price ?? 0,
  isDefault: record.is_default,
  createdAt: record.created_at,
  updatedAt: record.updated_at
});

const buildModelPricingPayload = (input = {}, fallbackModelName = '') => {
  const threshold = Number(input.longContextThreshold);
  return {
    // id 仅更新时有值；后端带 id 时按 id 精确定位，改 provider 或改档位也能命中原行
    id: Number(input.id) || 0,
    model_name: input.modelName || fallbackModelName,
    provider: input.provider || 'claude',
    service_tier: input.serviceTier || '',
    display_name: input.displayName || '',
    description: input.description || '',
    input_price: parseFloat(input.inputPrice) || 3.0,
    output_price: parseFloat(input.outputPrice) || 15.0,
    cache_creation_price_5m: parseFloat(input.cacheCreationPrice5m) || 0,
    cache_creation_price_1h: parseFloat(input.cacheCreationPrice1h) || 0,
    cache_read_price: parseFloat(input.cacheReadPrice) || 0,
    cache_write_price_30m: Number(input.cacheWritePrice30m) || 0,
    cache_write_price_12h: Number(input.cacheWritePrice12h) || 0,
    long_context_cache_write_price_12h: Number(input.longContextCacheWritePrice12h) || 0,
    long_context_threshold: Number.isSafeInteger(threshold) ? threshold : 0,
    long_context_input_price: parseFloat(input.longContextInputPrice) || 0,
    long_context_cache_read_price: parseFloat(input.longContextCacheReadPrice) || 0,
    long_context_cache_write_price: parseFloat(input.longContextCacheWritePrice) || 0,
    long_context_output_price: parseFloat(input.longContextOutputPrice) || 0,
    is_default: input.isDefault || false
  };
};

/**
 * 获取模型定价存储状态
 * @returns {Promise<Object>} - {enabled, totalCount, hasDefault}
 */
const getModelPricingStorageStatus = async () => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');

  const status = await WailsApp.GetModelPricingStorageStatus();
  return {
    enabled: status.enabled || false,
    totalCount: status.total_count || 0,
    hasDefault: status.has_default || false
  };
};

/**
 * 获取所有模型定价
 * @returns {Promise<Array>} - 模型定价数组
 */
const getModelPricings = async () => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');

  const records = await WailsApp.GetModelPricings();
  return (records || []).map(mapModelPricingRecord);
};

/**
 * 按 provider 获取模型定价列表
 * @param {string} provider - 提供商标识 (claude/codex)
 * @returns {Promise<Array>} - 模型定价数组
 */
const getModelPricingsByProvider = async (provider) => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');

  const records = await WailsApp.GetModelPricingsByProvider(provider || 'claude');
  return (records || []).map(mapModelPricingRecord);
};

/**
 * 获取单个模型定价
 * @param {string} modelName - 模型名称
 * @returns {Promise<Object>} - 模型定价记录
 */
const getModelPricing = async (modelName) => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');

  const r = await WailsApp.GetModelPricing(modelName);
  return mapModelPricingRecord(r);
};

/**
 * 创建模型定价
 * @param {Object} input - 定价配置
 * @returns {Promise<Object>} - {success: true}
 */
const createModelPricing = async (input) => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');

  const record = buildModelPricingPayload(input);

  await WailsApp.CreateModelPricing(record);
  return { success: true };
};

/**
 * 更新模型定价
 * @param {string} modelName - 模型名称
 * @param {Object} input - 更新的配置
 * @returns {Promise<Object>} - {success: true}
 */
const updateModelPricing = async (modelName, input) => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');

  const record = buildModelPricingPayload(input, modelName);

  await WailsApp.UpdateModelPricing(modelName, record);
  return { success: true };
};

/**
 * 删除模型定价（含该模型在该 provider 下的全部档位子行）
 * @param {string} provider - 提供商标识 (claude/codex)，两侧模型名空间会重叠，必须带
 * @param {string} modelName - 模型名称
 * @returns {Promise<Object>} - {success: true}
 */
const deleteModelPricing = async (provider, modelName) => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');

  await WailsApp.DeleteModelPricing(provider || 'claude', modelName);
  return { success: true };
};

/**
 * 删除单个档位子行，标准档主行不受影响
 * @param {string} provider - 提供商标识 (claude/codex)
 * @param {string} modelName - 模型名称
 * @param {string} serviceTier - 服务档位 (flex/batch/priority)
 * @returns {Promise<Object>} - {success: true}
 */
const deleteModelPricingTier = async (provider, modelName, serviceTier) => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');

  await WailsApp.DeleteModelPricingTier(provider || 'claude', modelName, serviceTier);
  return { success: true };
};

/**
 * 设置默认模型定价
 * @param {string} modelName - 模型名称
 * @returns {Promise<Object>} - {success: true}
 */
const setDefaultModelPricing = async (modelName) => {
  await initWails();
  if (!WailsApp) throw new Error('Wails not available');

  await WailsApp.SetDefaultModelPricing(modelName);
  return { success: true };
};

export { mapModelPricingRecord, buildModelPricingPayload, getModelPricingStorageStatus, getModelPricings, getModelPricingsByProvider, getModelPricing, createModelPricing, updateModelPricing, deleteModelPricing, deleteModelPricingTier, setDefaultModelPricing };
