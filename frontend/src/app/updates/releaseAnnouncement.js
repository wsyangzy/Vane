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
