import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

test('vite watcher keeps the child alive when Wails closes its output pipe', () => {
  const source = readFileSync(new URL('./vite-watch.mjs', import.meta.url), 'utf8');

  assert.doesNotMatch(source, /stdio:\s*['"]inherit['"]/);
  assert.match(source, /stdio:\s*\['ignore', 'pipe', 'pipe'\]/);
  assert.match(source, /process\.stdout\.on\('error'/);
  assert.match(source, /forwardOutput\(child\.stdout, 'stdout'\)/);
});

test('vite watcher registers only its wails3 dev ancestor for application-driven shutdown', () => {
  const source = readFileSync(new URL('./vite-watch.mjs', import.meta.url), 'utf8');

  assert.match(source, /\.wails-dev-watcher\.pid/);
  assert.match(source, /Get-CimInstance Win32_Process/);
  assert.match(source, /Name -eq 'wails3\.exe'/);
  assert.match(source, /CommandLine[\s\S]*-match '\(\^\|\\\\s\)dev\(\\\\s\|\$\)'/);
  assert.match(source, /writeFileSync\(watcherPIDFile, String\(watcherPID\)/);
  assert.match(source, /process\.on\('exit', clearWatcherPID\)/);
});

test('vite watcher starts Vite before the Windows watcher lookup can block startup', () => {
  const source = readFileSync(new URL('./vite-watch.mjs', import.meta.url), 'utf8');

  assert.match(source, /clearWatcherPID\(\);\s*start\(\);\s*setImmediate\(registerWatcherPID\);/);
});
