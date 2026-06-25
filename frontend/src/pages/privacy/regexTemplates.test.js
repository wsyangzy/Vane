import assert from 'node:assert/strict';
import test from 'node:test';
import { REGEX_TEMPLATES } from './regexTemplates.js';

const byName = Object.fromEntries(REGEX_TEMPLATES.map((template) => [template.name, template]));

const compileTemplate = (name) => {
  const pattern = byName[name]?.pattern;
  assert.ok(pattern, `missing template: ${name}`);
  if (pattern.startsWith('(?i)')) {
    return new RegExp(pattern.slice(4), 'i');
  }
  return new RegExp(pattern);
};

test('privacy regex templates match intended examples', () => {
  const cases = [
    ['手机号', '+86 13411110009'],
    ['邮箱', 'user.name+tag@example.co.uk'],
    ['身份证号', '420822199901011016'],
    ['银行卡号', '6222020000000197'],
    ['银行卡号', '6222020000000239'],
    ['JWT', 'eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjMifQ.signature'],
    ['私钥块', `-----BEGIN RSA PRIVATE KEY-----
abc
-----END RSA PRIVATE KEY-----`],
    ['数据库连接串', 'postgresql://user:pass@localhost:5432/db'],
    ['数据库连接串', 'mongodb+srv://user:pass@example.com/db'],
    ['内网 IP', '10.12.34.56'],
    ['内网 IP', '172.16.1.2'],
    ['内网 IP', '192.168.0.1'],
    ['MAC 地址', 'AA:bb:00:11:22:33'],
    ['MAC 地址', 'AA-bb-00-11-22-33'],
    ['统一社会信用代码', '91350211M000100Y43']
  ];

  for (const [name, value] of cases) {
    assert.match(value, compileTemplate(name), `${name} should match ${value}`);
  }
});

test('privacy regex templates reject shape-level false positives', () => {
  const cases = [
    ['手机号', '6222020000000213'],
    ['邮箱', 'user@-example.com'],
    ['身份证号', '6222020000000221'],
    ['银行卡号', '20150711004518'],
    ['银行卡号', '20240102092130'],
    ['银行卡号', '0000-000000000000'],
    ['数据库连接串', 'https://example.com/path'],
    ['内网 IP', '10.999.1.1'],
    ['内网 IP', '172.15.1.1'],
    ['内网 IP', '192.169.0.1'],
    ['MAC 地址', 'AA:bb-00:11:22:33'],
    ['统一社会信用代码', '91350211I000100Y43']
  ];

  for (const [name, value] of cases) {
    assert.doesNotMatch(value, compileTemplate(name), `${name} should reject ${value}`);
  }
});
