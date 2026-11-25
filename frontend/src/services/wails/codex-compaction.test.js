import test from 'node:test';
import assert from 'node:assert/strict';
import { mapCodexChannel, buildCodexChannelPayload } from './codex.js';

test('Codex compaction choices cross the channel mapping boundary independently', () => {
  for (const mode of ['preserve', 'native', 'summary']) {
    const channel = mapCodexChannel({ id: 1, name: 'anyrouter', compaction_mode: mode });
    assert.equal(channel.compactionMode, mode);
    assert.equal(buildCodexChannelPayload(channel).compaction_mode, mode);
  }
  assert.equal(mapCodexChannel({ id: 2, name: 'other' }).compactionMode, 'preserve');
  assert.equal(buildCodexChannelPayload({ compaction_mode: 'summary' }).compaction_mode, 'summary');
});

test('older channel callers omit the new field so existing choices survive edits', () => {
  assert.equal('compaction_mode' in buildCodexChannelPayload({ name: 'old caller' }), false);
  assert.equal(buildCodexChannelPayload({ compactionMode: '' }).compaction_mode, '');
});
