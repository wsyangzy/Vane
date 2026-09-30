package privacy

import (
	"fmt"
	"strings"
)

// 伪名化类别。决定替身值的生成格式。
const (
	pseudonymKindName     = "name"
	pseudonymKindPhone    = "phone"
	pseudonymKindIDCard   = "id_card"
	pseudonymKindBankCard = "bank_card"
	pseudonymKindSecret   = "secret"
	pseudonymKindToken    = "token"
	pseudonymKindAPIKey   = "api_key"
	pseudonymKindCustom   = "custom"
)

// IsValidPseudonymKind 校验伪名化类别
func IsValidPseudonymKind(kind string) bool {
	switch kind {
	case pseudonymKindName, pseudonymKindPhone, pseudonymKindIDCard, pseudonymKindBankCard,
		pseudonymKindSecret, pseudonymKindToken, pseudonymKindAPIKey, pseudonymKindCustom:
		return true
	default:
		return false
	}
}

// PseudonymKinds 返回全部可选类别（供前端下拉）
func PseudonymKinds() []string {
	return []string{
		pseudonymKindName, pseudonymKindPhone, pseudonymKindIDCard, pseudonymKindBankCard,
		pseudonymKindSecret, pseudonymKindToken, pseudonymKindAPIKey, pseudonymKindCustom,
	}
}

// pseudonymNames 假名池。用明显虚构的通用名，避免撞真实姓名。
var pseudonymNames = []string{
	"张三", "李四", "王五", "赵六", "钱七",
	"孙八", "周九", "吴十", "郑十一", "王十二",
}

// pseudonymizer 单次请求内的假名分配器。
//
// 刻意无状态：不持久化任何 raw→replacement 映射。
// Claude/Codex 的对话请求全量重发历史消息，因此「请求内一致」已等价于
// 「会话内一致」，足以让下游 AI 区分多个实体；而跨会话一致性的收益仅对
// AI 有意义（AI 本就不共享跨会话记忆），却要付出永久存储客户原文或其
// 哈希的代价——手机号、身份证的搜索空间有限，哈希可被枚举反推。
type pseudonymizer struct {
	assigned map[string]string
	counters map[string]int
}

func newPseudonymizer() *pseudonymizer {
	return &pseudonymizer{
		assigned: make(map[string]string),
		counters: make(map[string]int),
	}
}

// replacementFor 返回 raw 在本次请求内的稳定替身值。
func (p *pseudonymizer) replacementFor(kind, raw string) string {
	if !IsValidPseudonymKind(kind) {
		kind = pseudonymKindCustom
	}
	key := kind + "|" + raw
	if existing, ok := p.assigned[key]; ok {
		return existing
	}
	p.counters[kind]++
	replacement := pseudonymValue(kind, p.counters[kind], raw)
	p.assigned[key] = replacement
	return replacement
}

func pseudonymValue(kind string, index int, raw string) string {
	switch kind {
	case pseudonymKindName:
		return pseudonymName(index, raw)
	case pseudonymKindPhone:
		return pseudonymPhone(index)
	case pseudonymKindIDCard:
		return pseudonymIDCard(index)
	case pseudonymKindBankCard:
		return pseudonymBankCard(index)
	case pseudonymKindSecret:
		return fmt.Sprintf("fake_secret_%04d", index)
	case pseudonymKindToken:
		return fmt.Sprintf("fake_token_%04d", index)
	case pseudonymKindAPIKey:
		return fmt.Sprintf("fake_api_key_%04d", index)
	default:
		return fmt.Sprintf("fake_value_%04d", index)
	}
}

func pseudonymName(index int, raw string) string {
	if index <= len(pseudonymNames) {
		start := (index - 1) % len(pseudonymNames)
		for offset := 0; offset < len(pseudonymNames); offset++ {
			candidate := pseudonymNames[(start+offset)%len(pseudonymNames)]
			// 替身撞上原文等于没脱敏
			if candidate != raw {
				return candidate
			}
		}
	}
	for sequence := index - len(pseudonymNames); ; sequence++ {
		candidate := fmt.Sprintf("用户%04d", sequence)
		if candidate != raw {
			return candidate
		}
	}
}

// pseudonymPhone 使用固定保留前缀 1341111 + 4 位序号。
// 格式对下游校验合法，同时前缀固定使其可被识别为合成值。
func pseudonymPhone(index int) string {
	return fmt.Sprintf("1341111%04d", ((index-1)%10000)+1)
}

// pseudonymIDCard 固定地区码与出生日期 + 3 位序号 + 合法校验位。
func pseudonymIDCard(index int) string {
	base := fmt.Sprintf("42082219990101%03d", 101+((index-1)%899))
	return base + string(cnIDCheckChars[cnIDWeightedSum(base)%11])
}

func cnIDWeightedSum(base string) int {
	sum := 0
	for i := 0; i < 17 && i < len(base); i++ {
		if !isASCIIDigit(base[i]) {
			return 0
		}
		sum += int(base[i]-'0') * cnIDWeights[i]
	}
	return sum
}

// pseudonymBankCard 使用 UnionPay 前缀 + 4 位序号 + Luhn 校验位。
func pseudonymBankCard(index int) string {
	base := fmt.Sprintf("62220200000%04d", ((index-1)%10000)+1)
	return base + luhnCheckDigitFor(base)
}

func luhnCheckDigitFor(base string) string {
	for digit := byte('0'); digit <= '9'; digit++ {
		if validLuhn(base + string(digit)) {
			return string(digit)
		}
	}
	return "0"
}

// looksLikePseudonym 判断值是否为本引擎生成的替身值，
// 用于避免对已脱敏内容二次替换（同一会话历史会反复重发）。
//
// 判定必须与生成器逐位一致，不能只看前缀：只看前缀时，恰好以 1341111 开头的真实手机号
// 会被当作替身直接放行——那是一条真实的泄露路径。这里按生成器的全部约束反向校验：
// 手机号序号在 0001~9999、身份证序号在 101~999 且校验位吻合、银行卡序号与 Luhn 校验位吻合。
// 仍有极小概率撞上同格式的真实值（例如恰为 13411110001 的号码），这是避免二次替换的代价。
func looksLikePseudonym(value string) bool {
	if isFakeTokenPseudonym(value) {
		return true
	}
	switch {
	case len(value) == 11 && strings.HasPrefix(value, "1341111"):
		seq, ok := parseDigits(value[7:])
		return ok && seq >= 1 && seq <= 10000 && value == pseudonymPhone(seq)
	case len(value) == 18 && strings.HasPrefix(value, "42082219990101"):
		seq, ok := parseDigits(value[14:17])
		return ok && seq >= 101 && seq <= 999 && value == pseudonymIDCard(seq-100)
	case len(value) == 16 && strings.HasPrefix(value, "62220200000"):
		seq, ok := parseDigits(value[11:15])
		return ok && seq >= 1 && value == pseudonymBankCard(seq)
	}
	return false
}

// isFakeTokenPseudonym 只认 fake_<类别>_<4 位序号> 的完整形态；
// 任意以 fake_ 开头的真实密钥（例如某些测试环境的 key）不能因此被放行。
func isFakeTokenPseudonym(value string) bool {
	for _, prefix := range []string{"fake_secret_", "fake_token_", "fake_api_key_", "fake_value_"} {
		if strings.HasPrefix(value, prefix) {
			rest := value[len(prefix):]
			_, ok := parseDigits(rest)
			return ok && len(rest) == 4
		}
	}
	return false
}

func parseDigits(s string) (int, bool) {
	if s == "" {
		return 0, false
	}
	n := 0
	for i := 0; i < len(s); i++ {
		if !isASCIIDigit(s[i]) {
			return 0, false
		}
		n = n*10 + int(s[i]-'0')
	}
	return n, true
}
