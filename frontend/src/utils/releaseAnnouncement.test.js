import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

import {
  consumeReleaseAnnouncement,
  formatReleaseSections,
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
  sections: [
    { title: '凭证与成本', items: ['新增渠道成本限额'] },
    { title: '渠道与故障转移', items: ['优化故障转移一致性'] }
  ]
};

const sampleAnnouncement = {
  version: '8.5',
  sections: sampleReleaseNotes.sections
};

test('release announcement normalizes packaged version prefixes and dev suffixes', () => {
  assert.equal(normalizeReleaseVersion('v8.5'), '8.5');
  assert.equal(normalizeReleaseVersion('8.5-dev'), '8.5');
  assert.deepEqual(getReleaseAnnouncement('v8.5', sampleReleaseNotes), sampleAnnouncement);
});

test('release announcement is consumed only once for each version', () => {
  const storage = createStorage();

  assert.deepEqual(
    consumeReleaseAnnouncement({ runtimeVersion: '8.5', releaseNotes: sampleReleaseNotes, storage }),
    sampleAnnouncement
  );
  assert.equal(
    consumeReleaseAnnouncement({ runtimeVersion: '8.5', releaseNotes: sampleReleaseNotes, storage }),
    null
  );

  const nextRelease = {
    version: '8.6',
    sections: [{ title: '版本体验', items: ['新增请求分析视图'] }]
  };
  assert.deepEqual(
    consumeReleaseAnnouncement({ runtimeVersion: '8.6', releaseNotes: nextRelease, storage }),
    nextRelease
  );
});

test('release announcement accepts legacy highlights while rejecting stale or empty notes', () => {
  const storage = createStorage();
  const legacyNotes = { version: '8.5', highlights: ['兼容旧版发布说明'] };

  assert.deepEqual(getReleaseAnnouncement('8.5', legacyNotes), {
    version: '8.5',
    sections: [{ title: '本次更新', items: ['兼容旧版发布说明'] }]
  });
  assert.equal(
    consumeReleaseAnnouncement({ runtimeVersion: '8.6', releaseNotes: sampleReleaseNotes, storage }),
    null
  );
  assert.equal(consumeReleaseAnnouncement({
    runtimeVersion: '8.5',
    releaseNotes: { version: '8.5', sections: [] },
    storage
  }), null);
});

test('release sections are formatted as categorized toast lines', () => {
  assert.equal(
    formatReleaseSections([
      { title: '渠道与故障转移', items: ['完善初次配置', '优化并发切换'] },
      { title: '隐私防护', items: ['强化请求级脱敏'] }
    ]),
    '渠道与故障转移：完善初次配置；优化并发切换\n隐私防护：强化请求级脱敏'
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
  assert.ok(bundledReleaseNotes.sections.length > 0);
  assert.ok(bundledReleaseNotes.sections.every((section) => section.title && section.items.length > 0));
});
