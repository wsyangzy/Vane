import assert from 'node:assert/strict';
import test from 'node:test';
import { cleanLogMessage, escapeRegex, formatLogTime, formatLogValue, formatLogText, normalizeLogEntry } from './logPresentation.js';

test('log search treats regular expression symbols as literal text', () => {
  const query = '[proxy] (a+b)*?.$ ^{x}|\\';
  const regex = new RegExp(`(${escapeRegex(query)})`, 'gi');
  assert.deepEqual(`before ${query} after`.split(regex), ['before ', query, ' after']);
});

test('log values preserve strings, structured fields, zero, false and null', () => {
  assert.equal(formatLogValue('line 1\nline 2'), 'line 1\nline 2');
  assert.equal(formatLogValue({ duration: 0, success: false }), '{"duration":0,"success":false}');
  assert.equal(formatLogValue(['a', 'b']), '["a","b"]');
  assert.equal(formatLogValue(0), '0');
  assert.equal(formatLogValue(false), 'false');
  assert.equal(formatLogValue(null), 'null');
  assert.equal(formatLogValue(undefined), 'undefined');
});

test('missing or invalid log timestamps have an explicit empty display', () => {
  for (const timestamp of [null, undefined, '', 'invalid']) {
    assert.equal(formatLogTime(timestamp), '—');
    assert.equal(formatLogTime(timestamp, true), '—');
  }
});

test('log time uses millisecond precision and full time includes the date', () => {
  const timestamp = '2026-10-30T10:20:30.123+08:00';
  assert.match(formatLogTime(timestamp), /^\d{2}:\d{2}:\d{2}\.123$/);
  assert.match(formatLogTime(timestamp, true), /2026\/10\/30/);
});

test('copy and export preserve every field including nested objects and multiline messages', () => {
  const text = formatLogText({ timestamp: '2026-10-30T10:20:30+08:00', level: 'WARN', message: 'first\nsecond', attrs: { provider: 'codex', detail: { retry: 0 }, code: 0, enabled: false } });
  assert.match(text, /\[WARN\] first\nsecond \| provider=codex detail=\{"retry":0\} code=0 enabled=false$/);
  assert.doesNotMatch(text, /\[object Object\]/);
});

test('logs without extra fields do not gain a trailing separator', () => {
  assert.equal(formatLogText({ message: '' }), '[—] [INFO] ');
  assert.equal(formatLogText({ level: 'ERROR', message: 'failed', attrs: {} }), '[—] [ERROR] failed');
});

test('log messages remove decorative emoji while preserving indentation and line breaks', () => {
  assert.equal(cleanLogMessage('✅ 服务已启动'), '服务已启动');
  assert.equal(cleanLogMessage('  📡 监听事件\n⚠️ 准备重试\n❌ 请求失败'), '  监听事件\n准备重试\n请求失败');
  assert.equal(cleanLogMessage('过程 A 🔄 过程 B'), '过程 A 过程 B');
  assert.equal(cleanLogMessage('任务✅ 已完成'), '任务 已完成');
});

test('emoji cleanup handles joined symbols, modifiers, flags and keycaps without leaving control characters', () => {
  assert.equal(cleanLogMessage('🧑🏽‍💻 处理 👨‍👩‍👧‍👦 完成 🇨🇳 1️⃣ #️⃣ *️⃣'), '处理 完成 ');
  assert.equal(cleanLogMessage('🏴\u{E0067}\u{E0062}\u{E0065}\u{E006E}\u{E0067}\u{E007F} ready'), 'ready');
});

test('emoji cleanup preserves ordinary text symbols, numbers and identifiers', () => {
  const text = '状态 0 1 2 #42 * 10% ￥100 + - = → ← ≈ ≤ ≥ ©2026 ® ™';
  assert.equal(cleanLogMessage(text), text);
  assert.equal(cleanLogMessage('©️ 版权'), '版权');
});

test('normalizing a log only changes its message and retains original structured fields', () => {
  const attrs = Object.freeze({ channel: '渠道 🚀', detail: 'upstream ⚠️', count: 0 });
  const log = Object.freeze({ timestamp: '2026-10-30T10:20:30+08:00', level: 'INFO', message: '🚀 初始化完成', attrs });
  const normalized = normalizeLogEntry(log);
  assert.deepEqual(normalized, { ...log, message: '初始化完成' });
  assert.equal(normalized.attrs, attrs);
  assert.equal(log.message, '🚀 初始化完成');
  assert.match(formatLogText(log), /\[INFO\] 初始化完成 \| channel=渠道 🚀 detail=upstream ⚠️ count=0$/);
});

test('plain or invalid entries keep their identity for live selection tracking', () => {
  for (const log of [null, undefined, { message: null }, { message: '' }, { message: 'plain message', attrs: {} }]) {
    assert.equal(normalizeLogEntry(log), log);
  }
});
