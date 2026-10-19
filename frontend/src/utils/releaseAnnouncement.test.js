import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

import {
  buildReleaseToastSummary,
  getReleaseAnnouncement,
  normalizeReleaseVersion,
  RELEASE_SUMMARY_TOAST_DURATION_MS,
  showReleaseToastSummary
} from './releaseAnnouncement.js';

const sampleReleaseCatalog = {
  currentVersion: '8.5',
  releases: [
    {
      version: '8.5',
      sections: [
        { title: '凭证与成本', items: ['新增渠道成本限额'] },
        { title: '渠道与故障转移', items: ['优化故障转移一致性'] }
      ]
    },
    {
      version: '8.4',
      sections: [{ title: '数据管理', items: ['增加数据目录入口'] }]
    }
  ]
};

const sampleAnnouncement = {
  version: '8.5',
  sections: sampleReleaseCatalog.releases[0].sections
};

test('release announcement selects the running version from the archive', () => {
  assert.equal(normalizeReleaseVersion('v8.5'), '8.5');
  assert.equal(normalizeReleaseVersion('8.5-dev'), '8.5');
  assert.deepEqual(getReleaseAnnouncement('v8.5', sampleReleaseCatalog), sampleAnnouncement);
  assert.deepEqual(getReleaseAnnouncement('8.4', sampleReleaseCatalog), {
    version: '8.4',
    sections: sampleReleaseCatalog.releases[1].sections
  });
  assert.equal(getReleaseAnnouncement('8.3', sampleReleaseCatalog), null);
});

test('release announcement accepts the legacy single-version shape', () => {
  const legacyNotes = { version: '8.5', highlights: ['兼容旧版发布说明'] };

  assert.deepEqual(getReleaseAnnouncement('8.5', legacyNotes), {
    version: '8.5',
    sections: [{ title: '本次更新', items: ['兼容旧版发布说明'] }]
  });
  assert.equal(getReleaseAnnouncement('8.6', legacyNotes), null);
});

test('manual release summary displays all categories in one toast', () => {
  const announcement = {
    version: '8.5',
    sections: [
      { title: '渠道与故障转移', items: ['完善初次配置', '优化并发切换'] },
      { title: '隐私防护', items: ['强化请求级脱敏'] }
    ]
  };
  assert.deepEqual(buildReleaseToastSummary(announcement), {
    title: 'v8.5 更新内容',
    message: [
      '渠道与故障转移：完善初次配置',
      '渠道与故障转移：优化并发切换',
      '隐私防护：强化请求级脱敏'
    ]
  });

  const toastSource = readFileSync(new URL('../components/ui/Toast.jsx', import.meta.url), 'utf8');
  assert.match(toastSource, /Array\.isArray\(message\)/);
  assert.match(toastSource, /list-disc/);

  const shown = [];
  const dismissed = [];
  const stop = showReleaseToastSummary({
    announcement,
    showToast: (message, options) => shown.push({ message, options }),
    dismissToast: (toastId) => dismissed.push(toastId)
  });

  assert.equal(shown.length, 1);
  assert.equal(shown[0].options.duration, RELEASE_SUMMARY_TOAST_DURATION_MS);
  assert.equal(RELEASE_SUMMARY_TOAST_DURATION_MS, 12000);
  stop();
  assert.deepEqual(dismissed, [1]);
});

test('app startup never pops release notes; they only show on version click', () => {
  const appSource = readFileSync(new URL('../App.jsx', import.meta.url), 'utf8');

  assert.doesNotMatch(appSource, /release-notes\.json/);
  assert.doesNotMatch(appSource, /showReleaseToastSummary|ReleaseAnnouncement/);
});

test('bundled release catalog archives old versions and keeps current entries concise', () => {
  const catalog = JSON.parse(
    readFileSync(new URL('../release-notes.json', import.meta.url), 'utf8')
  );
  const globalsSource = readFileSync(new URL('../../../globals.go', import.meta.url), 'utf8');
  const versionMatch = globalsSource.match(/\bVersion\s*=\s*"([^"]+)"/);
  const versions = catalog.releases.map((release) => normalizeReleaseVersion(release.version));
  const currentAnnouncement = getReleaseAnnouncement(catalog.currentVersion, catalog);
  const summary = buildReleaseToastSummary(currentAnnouncement);

  assert.ok(versionMatch, 'globals.go must define Version');
  assert.equal(normalizeReleaseVersion(catalog.currentVersion), normalizeReleaseVersion(versionMatch[1]));
  assert.equal(new Set(versions).size, versions.length);
  assert.deepEqual(versions.slice(0, 3), ['8.8', '8.7', '8.6']);
  assert.equal(summary.message.length, 5);
  assert.ok(summary.message.every((item) => item.length <= 48));
});
