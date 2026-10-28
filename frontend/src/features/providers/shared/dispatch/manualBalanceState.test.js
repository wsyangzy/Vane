import test from 'node:test';
import assert from 'node:assert/strict';
import { balanceAmountError, balanceErrorMessage, formatUsd } from './manualBalanceState.js';

test('balance amounts retain precision and use explicit USD formatting', () => {
  assert.equal(formatUsd('0'), '$0.00');
  assert.equal(formatUsd('2000000000.000000001'), '$2,000,000,000.000000001');
  assert.equal(formatUsd('-0.000000001'), '$-0.000000001');
  assert.equal(balanceAmountError('0', 'calibrate'), '');
  assert.equal(balanceAmountError('20.000000001', 'credit'), '');
  for (const amount of ['-1', 'NaN', '1e3', '', '1.0000000001', '9223372037']) assert.ok(balanceAmountError(amount, 'calibrate'));
  assert.ok(balanceAmountError('0', 'credit'));
});

test('balance conflicts require a fresh read instead of silently applying a stale calibration', () => {
  assert.match(balanceErrorMessage(new Error('balance_revision_conflict')), /读取最新余额/);
  assert.match(balanceErrorMessage(new Error('balance_calibration_required')), /重新设置/);
});
