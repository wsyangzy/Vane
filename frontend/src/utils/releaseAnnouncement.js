const RELEASE_ANNOUNCEMENT_STORAGE_PREFIX = 'ai-relay-forwarder:release-announcement:';
const fallbackSeenVersions = new Set();

export const normalizeReleaseVersion = (value) => String(value ?? '')
  .trim()
  .replace(/^v/i, '')
  .replace(/-dev(?:[-.][\w.-]+)?$/i, '');

const normalizeHighlights = (highlights) => {
  if (!Array.isArray(highlights)) return [];

  return highlights
    .map((highlight) => String(highlight ?? '').trim())
    .filter(Boolean);
};

export const getReleaseAnnouncement = (runtimeVersion, releaseNotes) => {
  const normalizedRuntimeVersion = normalizeReleaseVersion(runtimeVersion);
  const normalizedNotesVersion = normalizeReleaseVersion(releaseNotes?.version);
  const highlights = normalizeHighlights(releaseNotes?.highlights);

  if (!normalizedRuntimeVersion || normalizedRuntimeVersion !== normalizedNotesVersion || highlights.length === 0) {
    return null;
  }

  return {
    version: normalizedNotesVersion,
    highlights
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

export const formatReleaseHighlights = (highlights) => normalizeHighlights(highlights)
  .map((highlight, index) => `${index + 1}. ${highlight}`)
  .join('\n');
