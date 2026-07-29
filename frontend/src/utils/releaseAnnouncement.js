const RELEASE_ANNOUNCEMENT_STORAGE_PREFIX = 'ai-relay-forwarder:release-announcement:';
const fallbackSeenVersions = new Set();

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

const getReleaseSections = (releaseNotes) => {
  const sections = normalizeSections(releaseNotes?.sections);
  if (sections.length > 0) return sections;

  const legacyHighlights = normalizeItems(releaseNotes?.highlights);
  return legacyHighlights.length > 0
    ? [{ title: '本次更新', items: legacyHighlights }]
    : [];
};

export const getReleaseAnnouncement = (runtimeVersion, releaseNotes) => {
  const normalizedRuntimeVersion = normalizeReleaseVersion(runtimeVersion);
  const normalizedNotesVersion = normalizeReleaseVersion(releaseNotes?.version);
  const sections = getReleaseSections(releaseNotes);

  if (!normalizedRuntimeVersion || normalizedRuntimeVersion !== normalizedNotesVersion || sections.length === 0) {
    return null;
  }

  return {
    version: normalizedNotesVersion,
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

export const formatReleaseSections = (sections) => normalizeSections(sections)
  .map((section) => `${section.title}：${section.items.join('；')}`)
  .join('\n');
