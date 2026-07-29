import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

import {
  consumeReleaseAnnouncement,
  formatReleaseHighlights,
  getReleaseAnnouncement,
  normalizeReleaseVersion
} from './releaseAnnouncement.js';

const createStorage = () => {
  const values = new Map();
  return {
    getItem: (key) => values.get(key) ?? null,
    setItem: (key, value) => values.set(key, value)
  };
};

const sampleReleaseNotes = {
  version: '8.5',
  highlights: ['新增渠道成本限额', '优化故障转移一致性']
};

test('release announcement normalizes packaged version prefixes and dev suffixes', () => {
  assert.equal(normalizeReleaseVersion('v8.5'), '8.5');
  assert.equal(normalizeReleaseVersion('8.5-dev'), '8.5');
  assert.deepEqual(getReleaseAnnouncement('v8.5', sampleReleaseNotes), sampleReleaseNotes);
});

test('release announcement is consumed only once for each version', () => {
  const storage = createStorage();

  assert.deepEqual(
    consumeReleaseAnnouncement({ runtimeVersion: '8.5', releaseNotes: sampleReleaseNotes, storage }),
    sampleReleaseNotes
  );
  assert.equal(
    consumeReleaseAnnouncement({ runtimeVersion: '8.5', releaseNotes: sampleReleaseNotes, storage }),
    null
  );

  const nextRelease = { version: '8.6', highlights: ['新增请求分析视图'] };
  assert.deepEqual(
    consumeReleaseAnnouncement({ runtimeVersion: '8.6', releaseNotes: nextRelease, storage }),
    nextRelease
  );
});

test('release announcement ignores stale or empty bundled notes', () => {
  const storage = createStorage();

  assert.equal(
    consumeReleaseAnnouncement({ runtimeVersion: '8.6', releaseNotes: sampleReleaseNotes, storage }),
    null
  );
  assert.equal(consumeReleaseAnnouncement({
    runtimeVersion: '8.5',
    releaseNotes: { version: '8.5', highlights: [] },
    storage
  }), null);
});

test('release highlights are formatted as readable toast lines', () => {
  assert.equal(
    formatReleaseHighlights(['新增渠道成本限额', '优化故障转移一致性']),
    '1. 新增渠道成本限额\n2. 优化故障转移一致性'
  );
});

test('bundled release notes stay aligned with the Go application version', () => {
  const bundledReleaseNotes = JSON.parse(
    readFileSync(new URL('../release-notes.json', import.meta.url), 'utf8')
  );
  const globalsSource = readFileSync(new URL('../../../globals.go', import.meta.url), 'utf8');
  const versionMatch = globalsSource.match(/\bVersion\s*=\s*"([^"]+)"/);

  assert.ok(versionMatch, 'globals.go must define Version');
  assert.equal(normalizeReleaseVersion(bundledReleaseNotes.version), normalizeReleaseVersion(versionMatch[1]));
  assert.ok(bundledReleaseNotes.highlights.length > 0);
});
