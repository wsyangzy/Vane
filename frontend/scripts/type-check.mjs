import { existsSync, readdirSync, statSync } from 'node:fs';
import { spawnSync } from 'node:child_process';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);
const projectRoot = path.resolve(__dirname, '..');

function walkForTsFiles(dir) {
  const result = [];

  for (const name of readdirSync(dir)) {
    if (name === 'node_modules' || name === 'dist' || name.startsWith('.')) {
      continue;
    }

    const fullPath = path.join(dir, name);
    const stat = statSync(fullPath);

    if (stat.isDirectory()) {
      result.push(...walkForTsFiles(fullPath));
      continue;
    }

    if (name.endsWith('.ts') || name.endsWith('.tsx')) {
      result.push(fullPath);
    }
  }

  return result;
}

const tsconfigPath = path.join(projectRoot, 'tsconfig.json');
const hasTsconfig = existsSync(tsconfigPath);
const tsFiles = walkForTsFiles(path.join(projectRoot, 'src'));

if (!hasTsconfig || tsFiles.length === 0) {
  console.log('[type-check] Skip: no TypeScript project configuration detected.');
  process.exit(0);
}

const result = spawnSync('pnpm', ['exec', 'tsc', '--noEmit'], {
  cwd: projectRoot,
  stdio: 'inherit',
  shell: true
});

process.exit(result.status ?? 1);
