import test from 'node:test';
import assert from 'node:assert/strict';

import {
  groupPricingsByModel,
  groupPricingSpeeds,
  buildTierPayloads,
  emptyTierValues,
  tierValuesFromRecord,
  serviceTierLabel
} from './pricingGroups.js';

test('基础定价的默认档位显示为 Standard，保存值仍为空档位', () => {
  assert.equal(serviceTierLabel(''), 'Standard');
});

test('Speed 聚合保留 Flex 与 Batch 独立价格，并包含 Ultrafast', () => {
  const groups = groupPricingSpeeds([
    { serviceTier: '', inputPrice: 2 },
    { serviceTier: 'flex', inputPrice: 1 },
    { serviceTier: 'batch', inputPrice: .8 },
    { serviceTier: 'fast', inputPrice: 4 },
    { serviceTier: 'ultrafast', inputPrice: 12 }
  ]);
  assert.deepEqual(groups.map((g) => g.label), ['Standard', 'Flex/Batch', 'Fast', 'Ultrafast']);
  assert.deepEqual(groups[1].records.map((r) => [r.serviceTier, r.inputPrice]), [['flex', 1], ['batch', .8]]);
});

test('保存档位计划携带 30m、12h 和长上下文写入价格', () => {
  const values = tierValuesFromRecord({ id: 7, cacheWritePrice30m: 5, cacheWritePrice12h: 10, longContextCacheWritePrice12h: 20 });
  const { saves } = buildTierPayloads({ provider: 'codex', modelName: 'test', tiers: { '': emptyTierValues(), ultrafast: values } });
  const ultra = saves.find((s) => s.serviceTier === 'ultrafast');
  assert.equal(ultra.cacheWritePrice30m, 5);
  assert.equal(ultra.cacheWritePrice12h, 10);
  assert.equal(ultra.longContextCacheWritePrice12h, 20);
  assert.equal(tierValuesFromRecord({ cacheWritePrice30m: 0, cacheCreationPrice5m: 8 }).cacheWritePrice30m, 0);
});

const row = (overrides) => ({
  id: 1,
  modelName: 'gpt-5',
  provider: 'codex',
  serviceTier: '',
  displayName: 'GPT-5',
  description: '',
  inputPrice: 1.25,
  outputPrice: 10,
  updatedAt: '2026-09-01',
  isDefault: false,
  ...overrides
});

test('同一模型的多个档位聚合成一张卡片，档位按标准顺序排列', () => {
  const groups = groupPricingsByModel([
    row({ id: 3, serviceTier: 'flex', inputPrice: 0.625 }),
    row({ id: 1, serviceTier: '', isDefault: true }),
    row({ id: 2, serviceTier: 'batch', inputPrice: 0.3 })
  ]);

  assert.equal(groups.length, 1);
  const [group] = groups;
  assert.equal(group.key, 'codex::gpt-5');
  assert.equal(group.standard.id, 1);
  assert.deepEqual(group.tiers.map((t) => t.serviceTier), ['batch', 'flex']);
  assert.equal(group.displayName, 'GPT-5');
  assert.equal(group.isDefault, true);
});

test('两侧同名模型不会并成一张卡片（跨 provider 数据隔离）', () => {
  const groups = groupPricingsByModel([
    row({ id: 1, modelName: 'MiniMax-M2', provider: 'claude', inputPrice: 3 }),
    row({ id: 2, modelName: 'MiniMax-M2', provider: 'codex', inputPrice: 1 })
  ]);

  assert.deepEqual(groups.map((g) => g.key), ['claude::MiniMax-M2', 'codex::MiniMax-M2']);
  assert.equal(groups[0].standard.inputPrice, 3);
  assert.equal(groups[1].standard.inputPrice, 1);
});

test('标准档缺失的历史脏数据仍可见可编辑', () => {
  const [group] = groupPricingsByModel([
    row({ id: 9, serviceTier: 'batch', displayName: '孤儿档位', updatedAt: '2026-08-08' })
  ]);

  assert.equal(group.standard, null);
  assert.equal(group.displayName, '孤儿档位');
  assert.equal(group.updatedAt, '2026-08-08');
  assert.equal(group.isDefault, false);
});

test('保存计划：启用的档位按 id 分流新建/更新，取消启用的档位进删除列表', () => {
  const { saves, deletes } = buildTierPayloads({
    modelName: 'gpt-5',
    displayName: 'GPT-5',
    description: '说明',
    provider: 'codex',
    isDefault: true,
    tiers: {
      '': { ...tierValuesFromRecord({ id: 1, inputPrice: 1.25, outputPrice: 10 }) },
      batch: { ...emptyTierValues(), enabled: true, inputPrice: 0.625, outputPrice: 5 },
      flex: { ...tierValuesFromRecord({ id: 7, inputPrice: 0.3 }), enabled: false },
      fast: { ...emptyTierValues() }
    }
  });

  assert.deepEqual(deletes, ['flex']);
  assert.deepEqual(saves.map((s) => [s.serviceTier, s.id]), [['', 1], ['batch', 0]]);

  // 模型级信息落到每一行，「默认」只落标准档
  assert.ok(saves.every((s) => s.modelName === 'gpt-5' && s.displayName === 'GPT-5'));
  assert.equal(saves[0].isDefault, true);
  assert.equal(saves[1].isDefault, false);
  // enabled 是表单态，不能混进后端载荷
  assert.ok(saves.every((s) => !('enabled' in s)));
});

test('标准档是兜底行，enabled=false 也照样保存', () => {
  const { saves, deletes } = buildTierPayloads({
    modelName: 'claude-sonnet-4',
    provider: 'claude',
    tiers: { '': { ...emptyTierValues(), id: 4, enabled: false, inputPrice: 3 } }
  });

  assert.deepEqual(deletes, []);
  assert.deepEqual(saves.map((s) => [s.serviceTier, s.id]), [['', 4]]);
});

test('非 Codex provider 不碰档位行：不新建也不误删历史子行', () => {
  const { saves, deletes } = buildTierPayloads({
    modelName: 'claude-sonnet-4',
    provider: 'claude',
    tiers: {
      '': { ...emptyTierValues(), id: 4, inputPrice: 3 },
      batch: { ...emptyTierValues(), enabled: true, inputPrice: 1.5 },
      flex: { ...tierValuesFromRecord({ id: 8 }), enabled: false }
    }
  });

  assert.deepEqual(deletes, []);
  assert.deepEqual(saves.map((s) => s.serviceTier), ['']);
});
