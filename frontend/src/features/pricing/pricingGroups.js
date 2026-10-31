// ============================================
// 定价分组与档位保存计划
// 纯函数，供定价表单、定价卡片与测试共用
// ============================================

// 定价表的档位取值：空串既是标准档定价也是其余档位的兜底，因此没有独立的 'standard' 选项
// （BillingRuleEditor 的 'standard' 是规则选择器语义，与定价行不是同一套取值）
export const PRICING_SERVICE_TIERS = [
  { value: '', label: 'Standard' },
  { value: 'batch', label: 'Batch' },
  { value: 'flex', label: 'Flex' },
  { value: 'fast', label: 'Fast' },
  { value: 'ultrafast', label: 'Ultrafast' }
];

export const serviceTierLabel = (tier) =>
  PRICING_SERVICE_TIERS.find((item) => item.value === tier)?.label || tier;

export const groupPricingSpeeds = (records = []) => {
  const groups = new Map();
  for (const record of records) {
    const key = ['batch', 'flex'].includes(record.serviceTier) ? 'flex_batch' : record.serviceTier || '';
    if (!groups.has(key)) groups.set(key, { key, label: key === 'flex_batch' ? 'Flex/Batch' : serviceTierLabel(key), records: [] });
    groups.get(key).records.push(record);
  }
  return [...groups.values()];
};

const tierOrder = (tier) => {
  const index = PRICING_SERVICE_TIERS.findIndex((item) => item.value === tier);
  return index < 0 ? PRICING_SERVICE_TIERS.length : index;
};

// 空档位价格。新增档位时用全 0，由调用方决定是否从标准档带入初值——
// 直接继承标准档价会让用户以为已配好，实际是拿标准价在给折扣档计费
export const emptyTierValues = () => ({
  id: 0,
  enabled: false,
  inputPrice: 0,
  outputPrice: 0,
  cacheCreationPrice5m: 0,
  cacheCreationPrice1h: 0,
  cacheWritePrice30m: 0,
  cacheWritePrice12h: 0,
  longContextCacheWritePrice12h: 0,
  cacheReadPrice: 0,
  longContextThreshold: 0,
  longContextInputPrice: 0,
  longContextCacheReadPrice: 0,
  longContextCacheWritePrice: 0,
  longContextOutputPrice: 0
});

// 从后端记录还原单个档位的价格字段
export const tierValuesFromRecord = (record = {}) => ({
  id: record.id || 0,
  enabled: true,
  inputPrice: record.inputPrice ?? 0,
  outputPrice: record.outputPrice ?? 0,
  cacheCreationPrice5m: record.cacheCreationPrice5m ?? 0,
  cacheCreationPrice1h: record.cacheCreationPrice1h ?? 0,
  cacheWritePrice30m: record.cacheWritePrice30m ?? record.cacheCreationPrice5m ?? 0,
  cacheWritePrice12h: record.cacheWritePrice12h ?? 0,
  longContextCacheWritePrice12h: record.longContextCacheWritePrice12h ?? 0,
  cacheReadPrice: record.cacheReadPrice ?? 0,
  longContextThreshold: record.longContextThreshold ?? 0,
  longContextInputPrice: record.longContextInputPrice ?? 0,
  longContextCacheReadPrice: record.longContextCacheReadPrice ?? 0,
  longContextCacheWritePrice: record.longContextCacheWritePrice ?? 0,
  longContextOutputPrice: record.longContextOutputPrice ?? 0
});

/**
 * 按 (provider, modelName) 把定价行聚合成「一个模型」。
 *
 * 档位不是模型的属性，而是定价的维度：同一个模型可以同时挂标准档 + batch + flex + fast
 * 四份价格（后端唯一键就是 (provider, model_name, service_tier) 三元组）。不聚合的话，
 * 一个模型会摊成多张卡片，让人误以为「一个模型只能选一个档位」，且 React key 会因同名重复。
 *
 * provider 必须参与分组键：两侧模型名空间会重叠，只按模型名聚合会把 Claude 与 Codex
 * 的同名模型并成一张卡片。
 */
export const groupPricingsByModel = (records = []) => {
  const groups = new Map();

  for (const record of records) {
    const provider = record.provider || 'claude';
    const key = `${provider}::${record.modelName}`;
    let group = groups.get(key);
    if (!group) {
      group = { key, provider, modelName: record.modelName, standard: null, tiers: [] };
      groups.set(key, group);
    }
    if (record.serviceTier) {
      group.tiers.push(record);
    } else {
      group.standard = record;
    }
  }

  for (const group of groups.values()) {
    group.tiers.sort((a, b) => tierOrder(a.serviceTier) - tierOrder(b.serviceTier));
    // 模型级信息取标准档；标准档缺失（历史脏数据）时退到第一条档位子行，
    // 至少让卡片可见、可编辑，否则这些行只能靠改库删掉
    const primary = group.standard || group.tiers[0] || {};
    group.displayName = primary.displayName || '';
    group.description = primary.description || '';
    group.updatedAt = primary.updatedAt || '';
    // 「默认」是 provider 级兜底行的属性，档位子行没有这个语义
    group.isDefault = Boolean(group.standard?.isDefault);
  }

  return Array.from(groups.values());
};

/**
 * 把表单的多档位数据拆成后端调用计划。
 *
 * 保存一次要落多行：启用的档位按 id 走新建或更新，取消启用且原本存在的档位要删除。
 *
 * @param {Object} form - { modelName, displayName, description, provider, isDefault, tiers }
 * @returns {{saves: Array, deletes: string[]}} saves 中 id>0 走更新、id===0 走新建
 */
export const buildTierPayloads = (form = {}) => {
  const saves = [];
  const deletes = [];
  const tiers = form.tiers || {};

  for (const { value: tier } of PRICING_SERVICE_TIERS) {
    // 档位子行仅 Codex 适用，其余 provider 只处理标准档，不误删历史子行
    if (tier !== '' && form.provider !== 'codex') continue;

    const values = tiers[tier];
    if (!values) continue;

    // 标准档是该模型的兜底行，永远存在，不可停用
    const enabled = tier === '' ? true : Boolean(values.enabled);
    if (!enabled) {
      if (values.id > 0) deletes.push(tier);
      continue;
    }

    const prices = { ...values };
    delete prices.enabled;
    saves.push({
      ...prices,
      id: values.id || 0,
      modelName: form.modelName,
      displayName: form.displayName,
      description: form.description,
      provider: form.provider,
      serviceTier: tier,
      // 「默认」是 provider 级兜底行的属性，档位子行不带这个标记
      isDefault: tier === '' ? Boolean(form.isDefault) : false
    });
  }

  return { saves, deletes };
};
