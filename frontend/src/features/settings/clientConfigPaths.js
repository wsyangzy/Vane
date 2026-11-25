const windowsPath = value => String(value || '').replaceAll('/', '\\');
const within = (path, root) => root && (path.toLowerCase() === root.toLowerCase() || path.toLowerCase().startsWith(`${root.toLowerCase()}\\`));

function pathRoots(path, context = {}) {
  const defaultDirectory = windowsPath(context?.defaultDirectory);
  const appData = /\\Vane\\backup\\?$/i.test(defaultDirectory) ? defaultDirectory.replace(/\\Vane\\backup\\?$/i, '') : '';
  const candidates = [windowsPath(context?.configPath), appData, path];
  const userProfile = appData.match(/^(.*)\\AppData\\Roaming$/i)?.[1]
    || candidates.map(value => value.match(/^([a-z]:\\Users\\[^\\]+)(?:\\|$)/i)?.[1]).find(Boolean) || '';
  return [
    ['%APPDATA%', appData || (userProfile ? `${userProfile}\\AppData\\Roaming` : '')],
    ['%LOCALAPPDATA%', userProfile ? `${userProfile}\\AppData\\Local` : ''],
    ['%USERPROFILE%', userProfile],
  ];
}

export function formatClientConfigPath(value, context) {
  if (!value) return '';
  if (!/^(?:[a-z]:[\\/]|\\\\|%)/i.test(value)) return value;
  const path = windowsPath(value);
  for (const [alias, root] of pathRoots(path, context)) {
    if (within(path, root)) return alias + path.slice(root.length);
  }
  return path;
}

// The setting still stores an absolute path; aliases are an editing/display aid.
export function expandClientConfigPath(value, context) {
  const path = windowsPath(value);
  for (const [alias, root] of pathRoots('', context)) {
    if (root && within(path, alias)) return root + path.slice(alias.length);
  }
  return value;
}
