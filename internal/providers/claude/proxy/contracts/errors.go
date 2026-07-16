package contracts

import "time"

// ErrorType is the single Claude proxy error classification contract.
type ErrorType int

const (
	ErrorTypeUnknown ErrorType = iota
	ErrorTypeNetwork
	ErrorTypeEOF
	ErrorTypeConnectionTimeout
	ErrorTypeResponseTimeout
	ErrorTypeTimeout
	ErrorTypeHTTP
	ErrorTypeServerError
	ErrorTypeStream
	ErrorTypeAuth
	ErrorTypeRateLimit
	ErrorTypeParsing
	ErrorTypeClientCancel
	ErrorTypeNoHealthyEndpoints
)

func (et ErrorType) String() string {
	switch et {
	case ErrorTypeNetwork:
		return "网络"
	case ErrorTypeEOF:
		return "EOF中断"
	case ErrorTypeConnectionTimeout:
		return "连接超时"
	case ErrorTypeResponseTimeout:
		return "响应超时"
	case ErrorTypeTimeout:
		return "超时"
	case ErrorTypeHTTP:
		return "HTTP"
	case ErrorTypeServerError:
		return "服务器"
	case ErrorTypeStream:
		return "流处理"
	case ErrorTypeAuth:
		return "认证"
	case ErrorTypeRateLimit:
		return "限流"
	case ErrorTypeParsing:
		return "解析"
	case ErrorTypeClientCancel:
		return "客户端取消"
	case ErrorTypeNoHealthyEndpoints:
		return "无健康端点"
	default:
		return "未知"
	}
}

type ErrorContext struct {
	RequestID      string
	EndpointName   string
	GroupName      string
	AttemptCount   int
	ErrorType      ErrorType
	OriginalError  error
	RetryableAfter time.Duration
	MaxRetries     int
}
