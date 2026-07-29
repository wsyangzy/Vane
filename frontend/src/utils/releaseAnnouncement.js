const RELEASE_ANNOUNCEMENT_STORAGE_PREFIX = 'ai-relay-forwarder:release-announcement:';
export const RELEASE_SUMMARY_TOAST_DURATION_MS = 12000;

const fallbackSeenVersions = new Set();
let stopActiveReleaseToastDisplay = null;

export const normalizeReleaseVersion = (value) => String(value ?? '')
  .trim()
  .replace(/^v/i, '')
  .replace(/-dev(?:[-.][\w.-]+)?$/i, '');

const normalizeItems = (items) => {
  if (!Array.isArray(items)) return [];

  return items
    .map((item) => String(item ?? '').trim())
    .filter(Boolean);
};

const normalizeSections = (sections) => {
  if (!Array.isArray(sections)) return [];

  return sections
    .map((section) => ({
      title: String(section?.title ?? '').trim() || '其他更新',
      items: normalizeItems(section?.items)
    }))
    .filter((section) => section.items.length > 0);
};

const getReleaseRecords = (releaseNotes) => {
  if (Array.isArray(releaseNotes?.releases)) {
    return releaseNotes.releases;
  }
  return releaseNotes?.version ? [releaseNotes] : [];
};

const getReleaseSections = (releaseRecord) => {
  const sections = normalizeSections(releaseRecord?.sections);
  if (sections.length > 0) return sections;

  const legacyHighlights = normalizeItems(releaseRecord?.highlights);
  return legacyHighlights.length > 0
    ? [{ title: '本次更新', items: legacyHighlights }]
    : [];
};

export const getReleaseAnnouncement = (runtimeVersion, releaseNotes) => {
  const normalizedRuntimeVersion = normalizeReleaseVersion(runtimeVersion);
  if (!normalizedRuntimeVersion) return null;

  const releaseRecord = getReleaseRecords(releaseNotes).find(
    (record) => normalizeReleaseVersion(record?.version) === normalizedRuntimeVersion
  );
  const sections = getReleaseSections(releaseRecord);
  if (!releaseRecord || sections.length === 0) return null;

  return {
    version: normalizedRuntimeVersion,
    sections
  };
};

const getPersistentStorage = () => {
  try {
    return typeof window !== 'undefined' ? window.localStorage : null;
  } catch {
    return null;
  }
};

export const consumeReleaseAnnouncement = ({ runtimeVersion, releaseNotes, storage } = {}) => {
  const announcement = getReleaseAnnouncement(runtimeVersion, releaseNotes);
  if (!announcement) return null;

  const storageKey = `${RELEASE_ANNOUNCEMENT_STORAGE_PREFIX}${announcement.version}`;
  const persistentStorage = storage === undefined ? getPersistentStorage() : storage;

  if (persistentStorage) {
    try {
      if (persistentStorage.getItem(storageKey) === '1') return null;
      persistentStorage.setItem(storageKey, '1');
      return announcement;
    } catch {
      // Fall through to process-local memory when WebView storage is unavailable.
    }
  }

  if (fallbackSeenVersions.has(announcement.version)) return null;
  fallbackSeenVersions.add(announcement.version);
  return announcement;
};

export const buildReleaseToastSummary = (announcement) => {
  const version = normalizeReleaseVersion(announcement?.version);
  const sections = normalizeSections(announcement?.sections);
  if (!version || sections.length === 0) return null;

  return {
    title: `v${version} 更新内容`,
    message: sections.flatMap((section) => (
      section.items.map((item) => `${section.title}：${item}`)
    ))
  };
};

export const showReleaseToastSummary = ({ announcement, showToast, dismissToast }) => {
  stopActiveReleaseToastDisplay?.();

  const summary = buildReleaseToastSummary(announcement);
  if (!summary || typeof showToast !== 'function') {
    stopActiveReleaseToastDisplay = null;
    return () => {};
  }

  let toastId = showToast(summary.message, {
    type: 'info',
    title: summary.title,
    duration: RELEASE_SUMMARY_TOAST_DURATION_MS
  });
  const stop = () => {
    if (toastId !== null && toastId !== undefined && typeof dismissToast === 'function') {
      dismissToast(toastId);
      toastId = null;
    }
    if (stopActiveReleaseToastDisplay === stop) {
      stopActiveReleaseToastDisplay = null;
    }
  };
  stopActiveReleaseToastDisplay = stop;
  return stop;
};
