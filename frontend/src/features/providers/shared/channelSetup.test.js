import assert from 'node:assert/strict';
import test from 'node:test';
import { createChannelSetupCommitter } from './channelSetup.js';

const setup = () => {
  const calls = [];
  const channel = { id: 11, name: 'Draft channel' };
  const group = { id: 23 };
  const operations = {
    create: async () => { calls.push('create'); },
    find: async () => { calls.push('find'); return channel; },
    saveGroup: async (value) => { assert.equal(value, channel); calls.push('group'); return group; },
    saveEndpoint: async (value, savedGroup) => {
      assert.equal(value, channel);
      assert.equal(savedGroup, group);
      calls.push('endpoint');
    },
    remove: async (value) => { assert.equal(value, channel); calls.push('remove'); }
  };
  return { calls, operations };
};

test('setup persists a channel, credential group, and endpoint in order', async () => {
  const { calls, operations } = setup();
  await createChannelSetupCommitter()(operations);
  assert.deepEqual(calls, ['create', 'find', 'group', 'endpoint']);
});

test('duplicate channel creation never deletes the existing channel', async () => {
  const { calls, operations } = setup();
  operations.create = async () => { calls.push('create'); throw new Error('name exists'); };
  await assert.rejects(createChannelSetupCommitter()(operations), /name exists/);
  assert.deepEqual(calls, ['create']);
});

for (const stage of ['saveGroup', 'saveEndpoint']) {
  test(`${stage} failure removes only the newly created channel, then allows a retry`, async () => {
    const { calls, operations } = setup();
    const save = operations[stage];
    operations[stage] = async () => { calls.push('fail'); throw new Error('save failed'); };
    const commit = createChannelSetupCommitter();
    await assert.rejects(commit(operations), /已撤销本次新增渠道/);
    assert.equal(calls.at(-1), 'remove');
    operations[stage] = save;
    calls.length = 0;
    await commit(operations);
    assert.deepEqual(calls, ['create', 'find', 'group', 'endpoint']);
  });
}

test('a failed channel lookup is retried for cleanup', async () => {
  const { calls, operations } = setup();
  const find = operations.find;
  let attempts = 0;
  operations.find = async () => {
    if (attempts++ === 0) { calls.push('find-failed'); throw new Error('read failed'); }
    return find();
  };
  await assert.rejects(createChannelSetupCommitter()(operations), /已撤销本次新增渠道/);
  assert.deepEqual(calls, ['create', 'find-failed', 'find', 'remove']);
});

test('a missing credential group ID aborts before creating an endpoint', async () => {
  const { calls, operations } = setup();
  operations.saveGroup = async () => ({ id: 0 });
  await assert.rejects(createChannelSetupCommitter()(operations), /未能读取新建凭证分组/);
  assert.deepEqual(calls, ['create', 'find', 'remove']);
});

test('cleanup failure blocks new writes until the previous channel can be removed', async () => {
  const { calls, operations } = setup();
  operations.saveEndpoint = async () => { throw new Error('save failed'); };
  operations.remove = async () => { calls.push('cleanup-failed'); throw new Error('delete failed'); };
  const commit = createChannelSetupCommitter();
  await assert.rejects(commit(operations), /自动清理未完成/);
  calls.length = 0;
  await assert.rejects(commit(operations), /上次创建的渠道尚未清理/);
  assert.deepEqual(calls, ['cleanup-failed']);
});

test('recovered cleanup runs before the next creation even if the next draft is different', async () => {
  const previous = setup();
  let cleanupFails = true;
  previous.operations.saveEndpoint = async () => { throw new Error('save failed'); };
  previous.operations.remove = async () => {
    previous.calls.push('cleanup');
    if (cleanupFails) throw new Error('delete failed');
  };
  const commit = createChannelSetupCommitter();
  await assert.rejects(commit(previous.operations), /自动清理未完成/);
  cleanupFails = false;
  previous.calls.length = 0;
  const next = setup();
  next.operations.create = async () => {
    assert.deepEqual(previous.calls, ['cleanup']);
    next.calls.push('create');
  };
  await commit(next.operations);
  assert.deepEqual(next.calls, ['create', 'find', 'group', 'endpoint']);
});

test('simultaneous final submissions cannot create duplicate channels', async () => {
  const { calls, operations } = setup();
  let release;
  const pending = new Promise((resolve) => { release = resolve; });
  operations.create = async () => { calls.push('create'); await pending; };
  const commit = createChannelSetupCommitter();
  const first = commit(operations);
  await assert.rejects(commit(operations), /正在创建渠道/);
  release();
  await first;
  assert.deepEqual(calls, ['create', 'find', 'group', 'endpoint']);
});
