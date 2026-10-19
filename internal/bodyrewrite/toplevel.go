// Package bodyrewrite 提供请求体的字节级改写工具。
//
// 反代必须忠实透传客户端请求：凡是允许改写请求体的环节（model 改写、路由改道），
// 都只能替换命中字段的字节区间，其余字节——对象键序、空白、转义形式、数字精度——
// 必须与客户端原文一致。把 body 解析成 map 再 json.Marshal 回去会重排键序、
// 把 < > & 转成 Unicode 转义，等于伪造客户端请求，可能触发提供商的指纹检测。
package bodyrewrite

import (
	"bytes"
	"encoding/json"
	"io"
)

// stringSpan 顶层对象中某个字符串取值 token（含双引号）的原始字节区间 [start, end)。
type stringSpan struct {
	start int
	end   int
	value string
}

// skipValue 让 Decoder 完整扫描并校验一个取值，但不复制、不解码其内容。
type skipValue struct{}

func (*skipValue) UnmarshalJSON([]byte) error { return nil }

// scanTopLevelString 扫描整个 body，返回顶层对象中 key 的全部取值区间。
//
// body 不是合法 JSON 对象、或任一同名取值不是字符串时返回 ok=false。
// 嵌套对象里的同名键不参与匹配；非目标键的取值只做语法校验、不解码，
// 大请求体（上百 KB 的 messages）也只多一次线性扫描。
func scanTopLevelString(body []byte, key string) (spans []stringSpan, ok bool) {
	dec := json.NewDecoder(bytes.NewReader(body))
	// 不把数字转成 float64：超出范围的大数字在转换时会报错，而这里只需要定位字节区间
	dec.UseNumber()

	first, err := dec.Token()
	if err != nil {
		return nil, false
	}
	if delim, isDelim := first.(json.Delim); !isDelim || delim != '{' {
		return nil, false
	}

	for dec.More() {
		keyToken, err := dec.Token()
		if err != nil {
			return nil, false
		}
		name, isString := keyToken.(string)
		if !isString {
			return nil, false
		}
		if name != key {
			if err := dec.Decode(&skipValue{}); err != nil {
				return nil, false
			}
			continue
		}

		// 键 token 末尾到取值末尾之间只有冒号与空白，因此首个双引号就是字符串 token 的起点
		keyEnd := dec.InputOffset()
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return nil, false
		}
		raw = bytes.TrimSpace(raw)
		if len(raw) == 0 || raw[0] != '"' {
			return nil, false
		}
		var value string
		if err := json.Unmarshal(raw, &value); err != nil {
			return nil, false
		}
		valueEnd := dec.InputOffset()
		quote := bytes.IndexByte(body[keyEnd:valueEnd], '"')
		if quote < 0 {
			return nil, false
		}
		spans = append(spans, stringSpan{start: int(keyEnd) + quote, end: int(valueEnd), value: value})
	}

	if _, err := dec.Token(); err != nil { // 顶层对象的 '}'
		return nil, false
	}
	if _, err := dec.Token(); err != io.EOF { // 顶层值之后不允许再有任何内容
		return nil, false
	}
	return spans, true
}

// LookupTopLevelString 返回顶层对象中 key 的字符串取值。
// 重复键取最后一个，与 encoding/json 的解码语义一致；
// 非 JSON、无该键、取值不是字符串时返回 ok=false。
func LookupTopLevelString(body []byte, key string) (value string, ok bool) {
	spans, ok := scanTopLevelString(body, key)
	if !ok || len(spans) == 0 {
		return "", false
	}
	return spans[len(spans)-1].value, true
}

// ReplaceTopLevelString 把顶层对象中 key 的字符串取值替换为 value，只改写该 token 的字节区间。
//
// 重复键全部替换（上游无论取哪一个都拿到新值），old 取最后一个原值。
// 非 JSON、无该键、取值不是字符串时原样返回 body 且 ok=false；
// 原值已等于 value 时也原样返回，保留客户端原有的转义形式。
// 替换值不做 HTML 转义。
func ReplaceTopLevelString(body []byte, key, value string) (out []byte, old string, ok bool) {
	spans, ok := scanTopLevelString(body, key)
	if !ok || len(spans) == 0 {
		return body, "", false
	}
	old = spans[len(spans)-1].value

	unchanged := true
	for _, span := range spans {
		if span.value != value {
			unchanged = false
			break
		}
	}
	if unchanged {
		return body, old, true
	}

	encoded := encodeJSONString(value)
	out = make([]byte, 0, len(body)+len(spans)*len(encoded))
	cursor := 0
	for _, span := range spans {
		out = append(out, body[cursor:span.start]...)
		out = append(out, encoded...)
		cursor = span.end
	}
	out = append(out, body[cursor:]...)
	return out, old, true
}

// encodeJSONString 返回带引号的 JSON 字符串 token。
// 关闭 HTML 转义：encoding/json 默认把 < > & 写成 < 这类形式，会改变上游收到的字节。
func encodeJSONString(value string) []byte {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(value) // 字符串编码不会失败
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n"))
}
