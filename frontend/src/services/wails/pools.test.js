import test from 'node:test';
import assert from 'node:assert/strict';
import { mapAccountPool, buildAccountPoolPayload } from './pools.js';

test('pool bridge preserves disabled migration rules, ordering and explicit account targets', () => {
  const dto = {
    id: 7, provider: 'codex', name: '默认池', strategy: 'round_robin', is_default: true, has_key: true, revision: 3,
    members: [{ channel_id: 3, group_id: 10, tier: 'backup' }, { channel_id: 2, group_id: 9, tier: 'primary' }],
    rules: [{ id: 1, name: '已关闭规则', disabled: true, priority: 8, match_model: 'gpt-*', mode: 'fallback', Targets: [{ channel_id: 3, group_id: 10, upstream_model: 'replacement' }, { pool_id: 8 }] }],
    non_stream: { mode: 'prefer', targets: [{ pool_id: 9 }] }
  };
  const mapped = mapAccountPool(dto);
  assert.equal(mapped.rules[0].disabled, true);
  assert.equal(mapped.hasKey, true);
  assert.deepEqual(mapped.rules[0].targets, [{ poolId: 0, channelId: 3, groupId: 10, upstreamModel: 'replacement' }, { poolId: 8, channelId: 0, groupId: 0, upstreamModel: '' }]);
  mapped.keyHash = 'never-submit-secret';
  mapped.rules[0].targets[1].rowId = 'local-react-key';
  const payload = buildAccountPoolPayload(mapped);
  assert.deepEqual(payload.members, dto.members);
  assert.equal(payload.rules[0].disabled, true);
  assert.equal(payload.rules[0].priority, 8);
  assert.deepEqual(payload.rules[0].Targets, [{ pool_id: 0, channel_id: 3, group_id: 10, upstream_model: 'replacement' }, { pool_id: 8, channel_id: 0, group_id: 0, upstream_model: '' }]);
  assert.equal(payload.non_stream.targets[0].pool_id, 9);
  assert.equal(JSON.stringify(payload).includes('never-submit-secret'), false);
  assert.equal(JSON.stringify(payload).includes('local-react-key'), false);
});
