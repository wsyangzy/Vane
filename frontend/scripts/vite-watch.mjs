import { spawn, spawnSync } from 'node:child_process';
import { rmSync, writeFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';

const viteBin = fileURLToPath(new URL('../node_modules/vite/bin/vite.js', import.meta.url));
const watcherPIDFile = fileURLToPath(new URL('../../build/bin/.wails-dev-watcher.pid', import.meta.url));
const viteArgs = process.argv.slice(2);
let child;
let stopping = false;
const outputOpen = { stdout: true, stderr: true };

const markOutputClosed = (name) => {
  outputOpen[name] = false;
};

process.stdout.on('error', () => markOutputClosed('stdout'));
process.stderr.on('error', () => markOutputClosed('stderr'));

const forwardOutput = (source, name) => {
  source.on('data', (chunk) => {
    if (!outputOpen[name]) return;
    try {
      process[name].write(chunk);
    } catch {
      markOutputClosed(name);
    }
  });
};

const report = (message) => {
  if (!outputOpen.stderr) return;
  try {
    process.stderr.write(`${message}\n`);
  } catch {
    markOutputClosed('stderr');
  }
};

const clearWatcherPID = () => {
  try {
    rmSync(watcherPIDFile, { force: true });
  } catch {
    // The file is best-effort development metadata.
  }
};

const findWailsDevWatcherPID = () => {
  if (process.platform !== 'win32') return 0;
  const script = `
$currentId = ${process.pid}
while ($currentId -gt 0) {
  $current = Get-CimInstance Win32_Process -Filter "ProcessId = $currentId" -ErrorAction SilentlyContinue
  if ($null -eq $current) { break }
  $commandLine = [string]$current.CommandLine
  if ($current.Name -eq 'wails3.exe' -and $commandLine -match '(^|\\s)dev(\\s|$)') {
    Write-Output $current.ProcessId
    break
  }
  $currentId = [int]$current.ParentProcessId
}
`;
  const result = spawnSync('powershell.exe', ['-NoProfile', '-NonInteractive', '-Command', script], {
    encoding: 'utf8',
    windowsHide: true
  });
  if (result.status !== 0) return 0;
  const pid = Number.parseInt(result.stdout.trim(), 10);
  return Number.isSafeInteger(pid) && pid > 0 ? pid : 0;
};

const registerWatcherPID = () => {
  const watcherPID = findWailsDevWatcherPID();
  if (!watcherPID) {
    report('[vite-watch] Unable to locate the Wails dev watcher process.');
    return;
  }
  try {
    writeFileSync(watcherPIDFile, String(watcherPID), { encoding: 'utf8', mode: 0o600 });
  } catch (error) {
    report(`[vite-watch] Unable to register the Wails dev watcher: ${error.message}`);
  }
};

const start = () => {
  if (stopping) return;
  child = spawn(process.execPath, [viteBin, ...viteArgs], {
    cwd: new URL('..', import.meta.url),
    stdio: ['ignore', 'pipe', 'pipe'],
    windowsHide: true
  });
  forwardOutput(child.stdout, 'stdout');
  forwardOutput(child.stderr, 'stderr');

  child.once('exit', (code, signal) => {
    child = undefined;
    if (stopping) return;
    report(`[vite-watch] Vite exited (${signal || code}); restarting in 1s...`);
    setTimeout(start, 1000);
  });
};

const stop = () => {
  if (stopping) return;
  stopping = true;
  clearWatcherPID();
  child?.kill();
};

process.on('SIGINT', stop);
process.on('SIGTERM', stop);
process.on('SIGHUP', stop);
process.on('exit', clearWatcherPID);

clearWatcherPID();
start();
setImmediate(registerWatcherPID);
