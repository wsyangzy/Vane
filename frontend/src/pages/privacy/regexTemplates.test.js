import assert from 'node:assert/strict';
import test from 'node:test';
import { REGEX_TEMPLATES } from './regexTemplates.js';

const byName = Object.fromEntries(REGEX_TEMPLATES.map((template) => [template.name, template]));

// 模板最终由后端 Go RE2 编译，这里用 JS RegExp 做形状校验。
// Go 的命名捕获组语法 (?P<name>...) 在 JS 中是 (?<name>...)，需转换后才能编译。
const compileTemplate = (name) => {
  let pattern = byName[name]?.pattern;
  assert.ok(pattern, `missing template: ${name}`);
  pattern = pattern.replaceAll('(?P<', '(?<');
  if (pattern.startsWith('(?i)')) {
    return new RegExp(pattern.slice(4), 'i');
  }
  return new RegExp(pattern);
};

test('privacy regex templates match intended examples', () => {
  const cases = [
    ['邮箱', 'user.name+tag@example.co.uk'],
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
    ['统一社会信用代码', '91350211M000100Y43'],
    ['字段值（保留字段名）', 'customer_name: 张伟']
  ];

  for (const [name, value] of cases) {
    assert.match(value, compileTemplate(name), `${name} should match ${value}`);
  }
});

test('privacy regex templates reject shape-level false positives', () => {
  const cases = [
    ['邮箱', 'user@-example.com'],
    ['数据库连接串', 'https://example.com/path'],
    ['内网 IP', '10.999.1.1'],
    ['内网 IP', '172.15.1.1'],
    ['内网 IP', '192.169.0.1'],
    ['MAC 地址', 'AA:bb-00:11:22:33'],
    ['统一社会信用代码', '91350211I000100Y43'],
    ['字段值（保留字段名）', 'customer_name:']
  ];

  for (const [name, value] of cases) {
    assert.doesNotMatch(value, compileTemplate(name), `${name} should reject ${value}`);
  }
});

// 手机号、身份证、银行卡刻意不在模板里：内置匹配器
// (builtin:cn_mobile / cn_id_card / bank_card_luhn) 带校验位与卡组织画像，
// 误报率远低于纯正则。留着正则版本会诱导用户选中误报更高的那个。
test('PII covered by builtin matchers is not offered as a regex template', () => {
  for (const name of ['手机号', '身份证号', '银行卡号']) {
    assert.equal(byName[name], undefined, `${name} should be handled by the builtin matcher, not a regex template`);
  }
});

// 只替换捕获组的模板必须真的带 redact 组，否则会连字段名一起脱敏。
test('field-value template exposes a redact capture group', () => {
  const pattern = byName['字段值（保留字段名）']?.pattern;
  assert.ok(pattern?.includes('(?P<redact>'), 'field-value template must carry a (?P<redact>...) group');

  const match = 'customer_name: 张伟'.match(compileTemplate('字段值（保留字段名）'));
  assert.equal(match?.groups?.redact, '张伟');
});
