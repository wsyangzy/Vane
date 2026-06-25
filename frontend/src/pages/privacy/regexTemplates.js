export const REGEX_TEMPLATES = [
  { name: '手机号', category: 'phone', pattern: String.raw`\b(?:\+?86[- ]?)?1[3-9]\d{9}\b` },
  { name: '邮箱', pattern: String.raw`\b[A-Za-z0-9](?:[A-Za-z0-9._%+-]{0,62}[A-Za-z0-9])?@(?:[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?\.)+[A-Za-z]{2,63}\b` },
  { name: '身份证号', category: 'id_card', pattern: String.raw`\b[1-9]\d{5}(?:18|19|20)\d{2}(?:0[1-9]|1[0-2])(?:0[1-9]|[12]\d|3[01])\d{3}[\dXx]\b` },
  { name: '银行卡号', category: 'bank_card', pattern: String.raw`\b(?:[3-9]\d{12,18}|[3-9]\d{3}(?:[- ]\d{4}){2,3}(?:[- ]\d{1,3})?)\b` },
  { name: 'JWT', category: 'token', pattern: String.raw`\b[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\b` },
  { name: '私钥块', category: 'secret', pattern: String.raw`-----BEGIN (?:[A-Z0-9]+ )?PRIVATE KEY-----[\s\S]*?-----END (?:[A-Z0-9]+ )?PRIVATE KEY-----` },
  { name: '数据库连接串', category: 'secret', pattern: String.raw`(?i)\b(?:jdbc:)?(?:mysql|mariadb|postgres(?:ql)?|mongodb(?:\+srv)?|redis|rediss|sqlserver|mssql)://[^\s"'<>，。；;]+` },
  { name: '内网 IP', pattern: String.raw`\b10\.(?:25[0-5]|2[0-4]\d|1?\d?\d)\.(?:25[0-5]|2[0-4]\d|1?\d?\d)\.(?:25[0-5]|2[0-4]\d|1?\d?\d)\b|\b172\.(?:1[6-9]|2\d|3[01])\.(?:25[0-5]|2[0-4]\d|1?\d?\d)\.(?:25[0-5]|2[0-4]\d|1?\d?\d)\b|\b192\.168\.(?:25[0-5]|2[0-4]\d|1?\d?\d)\.(?:25[0-5]|2[0-4]\d|1?\d?\d)\b` },
  { name: 'MAC 地址', pattern: String.raw`\b(?:[0-9A-Fa-f]{2}:){5}[0-9A-Fa-f]{2}\b|\b(?:[0-9A-Fa-f]{2}-){5}[0-9A-Fa-f]{2}\b` },
  { name: '统一社会信用代码', pattern: String.raw`\b[159Y][1239]\d{6}[0-9A-HJ-NPQRTUWXY]{10}\b` }
];
