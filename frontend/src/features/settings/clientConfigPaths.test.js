import test from 'node:test';
import assert from 'node:assert/strict';
import { expandClientConfigPath, formatClientConfigPath } from './clientConfigPaths.js';

const context = user => ({ defaultDirectory: `C:\\Users\\${user}\\AppData\\Roaming\\Vane\\backup`, configPath: `C:\\Users\\${user}\\.codex\\config.toml` });

test('backup and client paths have the same display across Windows users', () => {
  for (const user of ['alice', '张三', 'a user']) {
    const paths = context(user);
    assert.equal(formatClientConfigPath(paths.defaultDirectory, paths), '%APPDATA%\\Vane\\backup');
    assert.equal(formatClientConfigPath(paths.configPath, paths), '%USERPROFILE%\\.codex\\config.toml');
    assert.equal(formatClientConfigPath(`C:\\Users\\${user}\\.claude\\settings.json`, paths), '%USERPROFILE%\\.claude\\settings.json');
    assert.equal(formatClientConfigPath(`C:\\Users\\${user}\\AppData\\Local\\Vane`, paths), '%LOCALAPPDATA%\\Vane');
  }
});

test('path aliases honor redirected AppData, Windows case and path boundaries', () => {
  const paths = { defaultDirectory: 'D:\\Roaming\\Vane\\backup', configPath: 'C:\\Users\\alice\\.codex\\config.toml' };
  assert.equal(formatClientConfigPath('d:/roaming/Vane/backup/record', paths), '%APPDATA%\\Vane\\backup\\record');
  assert.equal(formatClientConfigPath('D:\\Roaming-old\\backup', paths), 'D:\\Roaming-old\\backup');
  assert.equal(formatClientConfigPath('C:\\Users\\alice2\\.codex\\config.toml', paths), 'C:\\Users\\alice2\\.codex\\config.toml');
  assert.equal(formatClientConfigPath('D:\\custom\\config.toml', paths), 'D:\\custom\\config.toml');
  assert.equal(formatClientConfigPath('\\\\server\\share\\backup', paths), '\\\\server\\share\\backup');
  assert.equal(formatClientConfigPath('/home/alice/.codex/config.toml', paths), '/home/alice/.codex/config.toml');
});

test('display works before settings load without mutating the actual directory', () => {
  const path = 'C:\\Users\\alice\\AppData\\Roaming\\Vane\\backup';
  assert.equal(formatClientConfigPath(path), '%APPDATA%\\Vane\\backup');
  assert.equal(formatClientConfigPath(path, null), '%APPDATA%\\Vane\\backup');
  assert.equal(formatClientConfigPath(''), '');
  assert.equal(formatClientConfigPath('%APPDATA%\\Vane\\backup'), '%APPDATA%\\Vane\\backup');
  assert.equal(path, 'C:\\Users\\alice\\AppData\\Roaming\\Vane\\backup');
});

test('editing aliases submits absolute paths and custom paths round trip unchanged', () => {
  const paths = context('alice');
  for (const path of [paths.defaultDirectory, paths.configPath, 'C:\\Users\\alice\\AppData\\Local\\Vane', 'D:\\Backups\\client']) {
    assert.equal(expandClientConfigPath(formatClientConfigPath(path, paths), paths), path);
  }
  assert.equal(expandClientConfigPath('%appdata%/Custom Backup', paths), 'C:\\Users\\alice\\AppData\\Roaming\\Custom Backup');
  assert.equal(expandClientConfigPath('%APPDATA%-other\\backup', paths), '%APPDATA%-other\\backup');
  assert.equal(expandClientConfigPath('%UNKNOWN%\\backup', paths), '%UNKNOWN%\\backup');
  assert.equal(expandClientConfigPath(''), '');
});
