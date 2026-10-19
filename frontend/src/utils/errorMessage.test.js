import test from 'node:test';
import assert from 'node:assert/strict';
import { getErrorMessage } from './errorMessage.js';

test('extracts Wails runtime messages from strings, objects and Error wrappers', () => {
  const message = '更新 Codex 端点失败: database is locked (5) (SQLITE_BUSY)';
  const runtimeError = { message, cause: {}, kind: 'RuntimeError' };
  const serialized = JSON.stringify(runtimeError);
  for (const error of [runtimeError, serialized, new Error(serialized), { message: serialized }, JSON.stringify(serialized)]) {
    assert.equal(getErrorMessage(error), message);
  }
});

test('preserves ordinary and malformed error text, including rollback details', () => {
  for (const message of ['请求失败\n回滚失败: 原因', '{invalid JSON', '网络连接失败']) {
    assert.equal(getErrorMessage(message), message);
    assert.equal(getErrorMessage(new Error(message)), message);
  }
});

test('uses the caller fallback for empty or unrecognized errors', () => {
  for (const error of [null, undefined, '', '  ', {}, { cause: {} }, new Error('')]) {
    assert.equal(getErrorMessage(error, '请求数据加载失败'), '请求数据加载失败');
  }
});
