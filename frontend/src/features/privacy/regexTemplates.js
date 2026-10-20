// 正则模板：仅保留内置规则（builtin:cn_mobile / cn_id_card / bank_card_luhn）未覆盖的项。
// 手机号、身份证、银行卡不在此列——内置匹配器带校验位与卡组织画像，误报率远低于纯正则。
export const REGEX_TEMPLATES = [
  { name: '邮箱', pattern: String.raw`\b[A-Za-z0-9](?:[A-Za-z0-9._%+-]{0,62}[A-Za-z0-9])?@(?:[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?\.)+[A-Za-z]{2,63}\b` },
  { name: '私钥块', pattern: String.raw`-----BEGIN (?:[A-Z0-9]+ )?PRIVATE KEY-----[\s\S]*?-----END (?:[A-Z0-9]+ )?PRIVATE KEY-----` },
  { name: '数据库连接串', pattern: String.raw`(?i)\b(?:jdbc:)?(?:mysql|mariadb|postgres(?:ql)?|mongodb(?:\+srv)?|redis|rediss|sqlserver|mssql)://[^\s"'<>，。；;]+` },
  { name: '内网 IP', pattern: String.raw`\b10\.(?:25[0-5]|2[0-4]\d|1?\d?\d)\.(?:25[0-5]|2[0-4]\d|1?\d?\d)\.(?:25[0-5]|2[0-4]\d|1?\d?\d)\b|\b172\.(?:1[6-9]|2\d|3[01])\.(?:25[0-5]|2[0-4]\d|1?\d?\d)\.(?:25[0-5]|2[0-4]\d|1?\d?\d)\b|\b192\.168\.(?:25[0-5]|2[0-4]\d|1?\d?\d)\.(?:25[0-5]|2[0-4]\d|1?\d?\d)\b` },
  { name: 'MAC 地址', pattern: String.raw`\b(?:[0-9A-Fa-f]{2}:){5}[0-9A-Fa-f]{2}\b|\b(?:[0-9A-Fa-f]{2}-){5}[0-9A-Fa-f]{2}\b` },
  { name: '统一社会信用代码', pattern: String.raw`\b[159Y][1239]\d{6}[0-9A-HJ-NPQRTUWXY]{10}\b` },
  { name: '字段值（保留字段名）', pattern: String.raw`(?i)\bcustomer_name\s*[:=：]\s*['"]?(?P<redact>[^'"\s,;]+)['"]?` }
];
