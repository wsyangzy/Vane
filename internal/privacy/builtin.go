package privacy

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// 内置匹配器标识。内置规则以 match_type='builtin' + pattern='builtin:xxx' 存表。
const (
	BuiltinEmail        = "builtin:email"
	BuiltinCNMobile     = "builtin:cn_mobile"
	BuiltinCNIDCard     = "builtin:cn_id_card"
	BuiltinBankCardLuhn = "builtin:bank_card_luhn"
	BuiltinCNLandline   = "builtin:cn_landline"
	BuiltinCNPassport   = "builtin:cn_passport"
	BuiltinCNCreditCode = "builtin:cn_credit_code"
	BuiltinPublicIPv4   = "builtin:public_ipv4"
)

// BuiltinMatcherInfo 内置匹配器的可选项（供前端新建规则时选择）
type BuiltinMatcherInfo struct {
	Pattern       string `json:"pattern"`
	Name          string `json:"name"`
	Description   string `json:"description"`
	Placeholder   string `json:"placeholder"`
	PseudonymKind string `json:"pseudonym_kind"`
}

// BuiltinMatchers 全部内置匹配器。默认种子只含误报率最低的几条，其余由用户按需启用。
func BuiltinMatchers() []BuiltinMatcherInfo {
	return []BuiltinMatcherInfo{
		{BuiltinCNIDCard, "中国身份证号", "18 位格式、省级地址码、合法出生日期与校验位", "[身份证]", pseudonymKindIDCard},
		{BuiltinBankCardLuhn, "银行卡号", "主流卡组织前缀/长度匹配，并通过 Luhn 校验", "[银行卡]", pseudonymKindBankCard},
		{BuiltinCNMobile, "中国手机号", "中国大陆手机号段格式，支持 +86 前缀", "[手机号]", pseudonymKindPhone},
		{BuiltinEmail, "邮箱地址", "排除 git@host 仓库地址与 logo@2x.png 一类资源名", "[邮箱]", pseudonymKindCustom},
		{BuiltinCNLandline, "中国座机号", "0 开头区号 + 7~8 位号码，区号与号码之间须有分隔符，可带分机号", "[座机]", pseudonymKindPhone},
		{BuiltinCNPassport, "中国护照号", "E/G 开头 + 8 位（新版 E + 字母 + 7 位），须出现在「护照」或 passport 字样附近", "[护照]", pseudonymKindCustom},
		{BuiltinCNCreditCode, "统一社会信用代码", "18 位，按 GB 32100 校验位验算", "[信用代码]", pseudonymKindCustom},
		{BuiltinPublicIPv4, "公网 IPv4 地址", "排除内网、回环、链路本地与保留地址段，以及版本号形态", "[IP]", pseudonymKindCustom},
	}
}

// builtinMatcher 返回文本中所有命中区间（字节偏移）
type builtinMatcher func(text string) [][2]int

var (
	emailBuiltinRe               = regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`)
	emailImageScaleAssetSuffixRe = regexp.MustCompile(`(?i)@[1-9][0-9]*(?:\.[0-9]+)?x\.(?:png|jpe?g|webp|gif|svg|avif|heic|heif|bmp|tiff?)$`)
	cnMobileBuiltinRe            = regexp.MustCompile(`(?:\+86)?(?:1740[0-5]\d{6}|1(?:[38]\d|4[57]|[59][0-35-9]|6[25-7]|7[0-35-8])\d{8})`)
	cnIDCardBuiltinRe            = regexp.MustCompile(`[1-9]\d{5}(?:18|19|20)\d{2}(?:0[1-9]|1[0-2])(?:0[1-9]|[12]\d|3[01])\d{3}[\dXx]`)
	bankCardBuiltinRe            = regexp.MustCompile(`\d{13,19}`)
)

// BuiltinPIIRules 返回默认种子化的内置规则。
// 都带强校验或强上下文约束，避免把订单号、时间戳、版本号一类数字串误判为 PII。
// 邮箱在编程场景里常出现在 git 配置与作者信息中，走三方中转时同样属于要保护的身份信息。
// 座机、护照、信用代码、公网 IP 误报面更大，不默认启用，由用户在界面按需新建。
func BuiltinPIIRules() []Rule {
	return []Rule{
		builtinRule("中国身份证号", "18 位格式、省级地址码、合法出生日期与校验位", BuiltinCNIDCard, "[身份证]", pseudonymKindIDCard, 20),
		builtinRule("银行卡号", "主流卡组织前缀/长度匹配，并通过 Luhn 校验", BuiltinBankCardLuhn, "[银行卡]", pseudonymKindBankCard, 30),
		builtinRule("中国手机号", "中国大陆手机号段格式，支持 +86 前缀", BuiltinCNMobile, "[手机号]", pseudonymKindPhone, 40),
		builtinRule("邮箱地址", "排除 git@host 仓库地址与 logo@2x.png 一类资源名", BuiltinEmail, "[邮箱]", pseudonymKindCustom, 50),
	}
}

// BuiltinRuleFor 按内置匹配器生成一条可新建的规则（source 为 custom，可删除）
func BuiltinRuleFor(pattern string) (Rule, bool) {
	for _, info := range BuiltinMatchers() {
		if info.Pattern == pattern {
			rule := builtinRule(info.Name, info.Description, info.Pattern, info.Placeholder, info.PseudonymKind, 60)
			rule.Source = SourceCustom
			return rule, true
		}
	}
	return Rule{}, false
}

func builtinRule(name, description, pattern, placeholder, pseudonymKind string, priority int) Rule {
	return Rule{
		Enabled:         true,
		Name:            name,
		Description:     description,
		Priority:        priority,
		MatchType:       MatchTypeBuiltin,
		Pattern:         pattern,
		Action:          ActionRedact,
		ReplacementMode: ReplacementPlaceholder,
		Placeholder:     placeholder,
		PseudonymKind:   pseudonymKind,
		Source:          SourceBuiltin,
	}
}

func compileBuiltinMatcher(pattern string) (builtinMatcher, error) {
	switch strings.TrimSpace(pattern) {
	case BuiltinEmail:
		return matchBuiltinEmail, nil
	case BuiltinCNMobile:
		return matchBuiltinCNMobile, nil
	case BuiltinCNIDCard:
		return matchBuiltinCNIDCard, nil
	case BuiltinBankCardLuhn:
		return matchBuiltinBankCardLuhn, nil
	case BuiltinCNLandline:
		return matchBuiltinCNLandline, nil
	case BuiltinCNPassport:
		return matchBuiltinCNPassport, nil
	case BuiltinCNCreditCode:
		return matchBuiltinCNCreditCode, nil
	case BuiltinPublicIPv4:
		return matchBuiltinPublicIPv4, nil
	default:
		return nil, fmt.Errorf("unknown builtin pattern: %s", pattern)
	}
}

// 座机：区号 0 开头 3~4 位 + 分隔符 + 7~8 位号码 + 可选分机。
// 强制要求分隔符（- 或空格或括号），否则 010 开头的任意 11 位数字串都会误判。
var cnLandlineBuiltinRe = regexp.MustCompile(`(?:\(0\d{2,3}\)\s?|0\d{2,3}[-\s])[2-9]\d{6,7}(?:[-转]\d{1,6})?`)

func matchBuiltinCNLandline(text string) [][2]int {
	locs := cnLandlineBuiltinRe.FindAllStringIndex(text, -1)
	out := make([][2]int, 0, len(locs))
	for _, loc := range locs {
		if hasDigitBoundary(text, loc[0], loc[1]) {
			continue
		}
		out = append(out, [2]int{loc[0], loc[1]})
	}
	return out
}

// 护照号本身格式很弱（E + 8 位与大量编号冲突），只在关键词附近命中
var (
	cnPassportBuiltinRe  = regexp.MustCompile(`\b[EG][A-Z0-9]\d{7}\b`)
	cnPassportKeywordRe  = regexp.MustCompile(`(?i)护照|passport`)
	passportContextBytes = 32
)

func matchBuiltinCNPassport(text string) [][2]int {
	locs := cnPassportBuiltinRe.FindAllStringIndex(text, -1)
	out := make([][2]int, 0, len(locs))
	for _, loc := range locs {
		start := loc[0] - passportContextBytes
		if start < 0 {
			start = 0
		}
		if !cnPassportKeywordRe.MatchString(text[start:loc[0]]) {
			continue
		}
		out = append(out, [2]int{loc[0], loc[1]})
	}
	return out
}

// 统一社会信用代码：18 位，字符集为数字与除 I O Z S V 外的大写字母，末位为 GB 32100 校验位
var cnCreditCodeBuiltinRe = regexp.MustCompile(`\b[0-9A-HJ-NPQRTUWXY]{2}\d{6}[0-9A-HJ-NPQRTUWXY]{10}\b`)

const cnCreditCodeCharset = "0123456789ABCDEFGHJKLMNPQRTUWXY"

var cnCreditCodeWeights = [17]int{1, 3, 9, 27, 19, 26, 16, 17, 20, 29, 25, 13, 8, 24, 10, 30, 28}

func matchBuiltinCNCreditCode(text string) [][2]int {
	locs := cnCreditCodeBuiltinRe.FindAllStringIndex(text, -1)
	out := make([][2]int, 0, len(locs))
	for _, loc := range locs {
		if validCNCreditCode(text[loc[0]:loc[1]]) {
			out = append(out, [2]int{loc[0], loc[1]})
		}
	}
	return out
}

func validCNCreditCode(value string) bool {
	if len(value) != 18 {
		return false
	}
	sum := 0
	for i := 0; i < 17; i++ {
		idx := strings.IndexByte(cnCreditCodeCharset, value[i])
		if idx < 0 {
			return false
		}
		sum += idx * cnCreditCodeWeights[i]
	}
	check := (31 - sum%31) % 31
	return value[17] == cnCreditCodeCharset[check]
}

var publicIPv4BuiltinRe = regexp.MustCompile(`\b(?:25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)(?:\.(?:25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)){3}\b`)

// matchBuiltinPublicIPv4 只命中公网地址：内网、回环、保留段对第三方没有识别意义，
// 却在代码与配置里大量出现，全部脱敏只会干扰模型理解。
// 前后紧邻点号或字母数字时视为版本号 / 更长标识的一部分（如 1.2.3.4.5、v1.2.3.4）。
func matchBuiltinPublicIPv4(text string) [][2]int {
	locs := publicIPv4BuiltinRe.FindAllStringIndex(text, -1)
	out := make([][2]int, 0, len(locs))
	for _, loc := range locs {
		if loc[0] > 0 && (text[loc[0]-1] == '.' || isASCIIAlnum(text[loc[0]-1])) {
			continue
		}
		if loc[1] < len(text) && text[loc[1]] == '.' && loc[1]+1 < len(text) && isASCIIDigit(text[loc[1]+1]) {
			continue
		}
		if !isPublicIPv4(text[loc[0]:loc[1]]) {
			continue
		}
		out = append(out, [2]int{loc[0], loc[1]})
	}
	return out
}

func isPublicIPv4(value string) bool {
	parts := strings.Split(value, ".")
	if len(parts) != 4 {
		return false
	}
	var octets [4]int
	for i, part := range parts {
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 || n > 255 {
			return false
		}
		octets[i] = n
	}
	a, b := octets[0], octets[1]
	switch {
	case a == 0, a == 10, a == 127, a >= 224:
		return false
	case a == 100 && b >= 64 && b <= 127: // CGNAT
		return false
	case a == 169 && b == 254:
		return false
	case a == 172 && b >= 16 && b <= 31:
		return false
	case a == 192 && b == 168:
		return false
	case a == 192 && b == 0:
		return false
	case a == 198 && (b == 18 || b == 19):
		return false
	}
	return true
}

func isASCIIAlnum(b byte) bool {
	return isASCIIDigit(b) || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

func matchBuiltinEmail(text string) [][2]int {
	locs := emailBuiltinRe.FindAllStringIndex(text, -1)
	out := make([][2]int, 0, len(locs))
	for _, loc := range locs {
		// git@host:path 与 ssh/scp 参数不是邮箱
		if isCommandAddressContext(text, loc[0], loc[1]) {
			continue
		}
		// logo@2x.png 这类资源名不是邮箱
		if isImageScaleAssetName(text[loc[0]:loc[1]]) {
			continue
		}
		out = append(out, [2]int{loc[0], loc[1]})
	}
	return out
}

func isImageScaleAssetName(value string) bool {
	return emailImageScaleAssetSuffixRe.MatchString(value)
}

func isCommandAddressContext(text string, start, end int) bool {
	if end < len(text) && text[end] == ':' && strings.HasPrefix(strings.ToLower(text[start:end]), "git@") {
		return true
	}
	lineStart := strings.LastIndexByte(text[:start], '\n') + 1
	prefix := strings.ToLower(strings.TrimSpace(text[lineStart:start]))
	if prefix == "" {
		return false
	}
	fields := strings.Fields(prefix)
	if len(fields) == 0 {
		return false
	}
	last := fields[len(fields)-1]
	return last == "ssh" || last == "scp"
}

func matchBuiltinCNMobile(text string) [][2]int {
	locs := cnMobileBuiltinRe.FindAllStringIndex(text, -1)
	out := make([][2]int, 0, len(locs))
	for _, loc := range locs {
		if hasDigitBoundary(text, loc[0], loc[1]) {
			continue
		}
		out = append(out, [2]int{loc[0], loc[1]})
	}
	return out
}

func matchBuiltinCNIDCard(text string) [][2]int {
	locs := cnIDCardBuiltinRe.FindAllStringIndex(text, -1)
	out := make([][2]int, 0, len(locs))
	for _, loc := range locs {
		if hasDigitBoundary(text, loc[0], loc[1]) {
			continue
		}
		if !validCNIDCard(text[loc[0]:loc[1]]) {
			continue
		}
		out = append(out, [2]int{loc[0], loc[1]})
	}
	return out
}

func matchBuiltinBankCardLuhn(text string) [][2]int {
	locs := bankCardBuiltinRe.FindAllStringIndex(text, -1)
	out := make([][2]int, 0, len(locs))
	for _, loc := range locs {
		if hasDigitBoundary(text, loc[0], loc[1]) {
			continue
		}
		value := text[loc[0]:loc[1]]
		if !matchesKnownPaymentCardProfile(value) {
			continue
		}
		if !validLuhn(value) {
			continue
		}
		out = append(out, [2]int{loc[0], loc[1]})
	}
	return out
}

// matchesKnownPaymentCardProfile 按卡组织的前缀与长度画像过滤。
// 仅靠 Luhn 会把大量普通数字串（约 1/10 随机命中）误判为银行卡。
func matchesKnownPaymentCardProfile(value string) bool {
	length := len(value)
	if length < 13 || length > 19 {
		return false
	}
	return matchesVisa(value, length) ||
		matchesMastercard(value, length) ||
		matchesAmericanExpress(value, length) ||
		matchesDinersClub(value, length) ||
		matchesDiscover(value, length) ||
		matchesJCB(value, length) ||
		matchesUnionPay(value, length)
}

func matchesVisa(value string, length int) bool {
	return strings.HasPrefix(value, "4") && hasLength(length, 13, 16, 18, 19)
}

func matchesMastercard(value string, length int) bool {
	if length != 16 {
		return false
	}
	if prefix2 := prefixInt(value, 2); prefix2 >= 51 && prefix2 <= 55 {
		return true
	}
	prefix4 := prefixInt(value, 4)
	return prefix4 >= 2221 && prefix4 <= 2720
}

func matchesAmericanExpress(value string, length int) bool {
	if length != 15 {
		return false
	}
	prefix2 := prefixInt(value, 2)
	return prefix2 == 34 || prefix2 == 37
}

func matchesDinersClub(value string, length int) bool {
	if length != 14 {
		return false
	}
	prefix2 := prefixInt(value, 2)
	prefix3 := prefixInt(value, 3)
	return (prefix3 >= 300 && prefix3 <= 305) || prefix2 == 36 || prefix2 == 38 || prefix2 == 39
}

func matchesDiscover(value string, length int) bool {
	if !hasLength(length, 16, 19) {
		return false
	}
	prefix2 := prefixInt(value, 2)
	prefix3 := prefixInt(value, 3)
	prefix4 := prefixInt(value, 4)
	return prefix4 == 6011 || (prefix3 >= 644 && prefix3 <= 649) || prefix2 == 65
}

func matchesJCB(value string, length int) bool {
	prefix4 := prefixInt(value, 4)
	if length == 15 && (prefix4 == 2131 || prefix4 == 1800) {
		return true
	}
	return length >= 16 && length <= 19 && prefix4 >= 3528 && prefix4 <= 3589
}

func matchesUnionPay(value string, length int) bool {
	if length < 14 || length > 19 {
		return false
	}
	prefix3 := prefixInt(value, 3)
	if prefix3 == 620 || (prefix3 >= 623 && prefix3 <= 626) || prefix3 == 810 {
		return true
	}
	prefix4 := prefixInt(value, 4)
	if prefix4 == 6270 || prefix4 == 6272 || prefix4 == 6276 ||
		(prefix4 >= 6282 && prefix4 <= 6289) || prefix4 == 6291 || prefix4 == 6292 ||
		(prefix4 >= 8110 && prefix4 <= 8171) {
		return true
	}
	prefix5 := prefixInt(value, 5)
	if (prefix5 >= 62100 && prefix5 <= 62182) ||
		(prefix5 >= 62184 && prefix5 <= 62197) ||
		(prefix5 >= 62200 && prefix5 <= 62205) ||
		(prefix5 >= 62207 && prefix5 <= 62209) {
		return true
	}
	prefix6 := prefixInt(value, 6)
	return (prefix6 >= 622010 && prefix6 <= 622999) ||
		(prefix6 >= 627700 && prefix6 <= 627779) ||
		(prefix6 >= 627781 && prefix6 <= 627799)
}

func hasLength(length int, allowed ...int) bool {
	for _, item := range allowed {
		if length == item {
			return true
		}
	}
	return false
}

func prefixInt(value string, length int) int {
	if len(value) < length {
		return -1
	}
	prefix, err := strconv.Atoi(value[:length])
	if err != nil {
		return -1
	}
	return prefix
}

// hasDigitBoundary 命中区间紧邻数字时判为更长数字串的一部分（如订单号中的片段），不算 PII。
func hasDigitBoundary(text string, start, end int) bool {
	return (start > 0 && isASCIIDigit(text[start-1])) || (end < len(text) && isASCIIDigit(text[end]))
}

func isASCIIDigit(b byte) bool {
	return b >= '0' && b <= '9'
}

func validCNIDCard(value string) bool {
	if len(value) != 18 {
		return false
	}
	if !validCNIDProvinceCode(value[:2]) {
		return false
	}
	birth := value[6:14]
	parsed, err := time.Parse("20060102", birth)
	if err != nil || parsed.Format("20060102") != birth {
		return false
	}
	sum := 0
	for i := 0; i < 17; i++ {
		if !isASCIIDigit(value[i]) {
			return false
		}
		sum += int(value[i]-'0') * cnIDWeights[i]
	}
	got := value[17]
	if got == 'x' {
		got = 'X'
	}
	return got == cnIDCheckChars[sum%11]
}

var (
	cnIDWeights    = [17]int{7, 9, 10, 5, 8, 4, 2, 1, 6, 3, 7, 9, 10, 5, 8, 4, 2}
	cnIDCheckChars = [11]byte{'1', '0', 'X', '9', '8', '7', '6', '5', '4', '3', '2'}
)

func validCNIDProvinceCode(value string) bool {
	switch value {
	case "11", "12", "13", "14", "15",
		"21", "22", "23",
		"31", "32", "33", "34", "35", "36", "37",
		"41", "42", "43", "44", "45", "46",
		"50", "51", "52", "53", "54",
		"61", "62", "63", "64", "65",
		"71", "81", "82":
		return true
	default:
		return false
	}
}

func validLuhn(value string) bool {
	sum := 0
	double := false
	for i := len(value) - 1; i >= 0; i-- {
		if !isASCIIDigit(value[i]) {
			return false
		}
		digit := int(value[i] - '0')
		if double {
			digit *= 2
			if digit > 9 {
				digit -= 9
			}
		}
		sum += digit
		double = !double
	}
	return sum > 0 && sum%10 == 0
}
