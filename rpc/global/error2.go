package global

import (
	"errors"

	"go.starlark.net/lib/proto"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var ParamErr = errors.New("params not adapted")

// Error error.
type Error interface {
	error
	// Code get error code.
	Code() int
	Equal(error) bool
}

type ecode codes.Code

type ErrorInfo struct {
	Err   error
	Codee codes.Code
}

func (e ErrorInfo) Error() string {
	return e.Err.Error()
}

func (e ErrorInfo) Code() int {
	return int(e.Codee)
}

func (e ErrorInfo) Equal(err error) bool {
	if errCode, ok := err.(Error); ok {
		return e.Code() == errCode.Code()
	}
	return false
}

// ecode error.

// 自定义错误状态码
const (
	OK                 ecode = 0 //
	Canceled           ecode = 1 //  表示操作被取消
	Unknown            ecode = 2 //  表示发生了未知的错误。
	InvalidArgument    ecode = 3 //  表示客户端提供的参数无效
	DeadlineExceeded   ecode = 4
	NotFound           ecode = 5
	AlreadyExists      ecode = 6 //  请求的资源已经存在
	PermissionDenied   ecode = 7 //  表示客户端没有足够的权限执行请求的操作
	ResourceExhausted  ecode = 8 //  表示资源已经耗尽，例如超过了配额限制
	FailedPrecondition ecode = 9
	Aborted            ecode = 10
	OutOfRange         ecode = 11
	Unimplemented      ecode = 12
	Internal           ecode = 13
	Unavailable        ecode = 14
	DataLoss           ecode = 15
	Unauthenticated    ecode = 16
	NotModified        ecode = 17
	ParamsErr          ecode = 18
	NothingFound       ecode = 19
	Conflict           ecode = 20
	ServerErr          ecode = 21
	paramErr           ecode = 22 // 参数错误
	NetworkErr         ecode = 23 // 网络错误
)

var errorCodeMap = map[int]string{
	0:  "OK",
	1:  "Canceled",
	2:  "Unknown",
	3:  "InvalidArgument",
	4:  "DeadlineExceeded",
	5:  "NotFound",
	6:  "AlreadyExists",
	7:  "PermissionDenied",
	8:  "ResourceExhausted",
	9:  "FailedPrecondition",
	10: "Aborted",
	11: "OutOfRange",
	12: "Unimplemented",
	13: "Internal",
	14: "Unavailable",
	15: "DataLoss",
	16: "Unauthenticated",
	17: "NotModified",
	18: "ParamsErr",
	19: "NothingFound",
	20: "Conflict",
	21: "ServerErr",
	22: "paramErr",
	23: "NetworkErr",
	24: "ClientCallErr",
}

func GetErrorInfo(e int) string {
	if msg, ok := errorCodeMap[e]; ok {
		return msg
	}
	return "未知错误"
}
func (ec ecode) Error() string {
	if msg, ok := errorCodeMap[ec.Code()]; ok {
		return msg
	}
	return "未知错误"
}

func (ec ecode) Code() int {
	return int(ec)
}

func (ec ecode) Equal(err error) bool {
	if ec, ok := err.(Error); ok {
		return ec == err
	}
	return false
}

// AuthError 权限相关错误
type AuthError int

const (
	ErrAuthenticationFailed      AuthError = 1401 // 认证失败
	ErrMissingToken              AuthError = 1402 // 缺少令牌
	ErrTokenExpired              AuthError = 1403 // 令牌过期
	ErrInvalidUsernameOrPassword AuthError = 1404 // 无效的用户名或密码
	ErrFailedToGetStoredPassword AuthError = 1501 // 无法获取存储的密码
	ErrFailedToGetExpirationTime AuthError = 1502 // 无法获取过期时间
)

// StatusError represents an error with additional status details.
type StatusError struct {
	Code    Error
	Message string
	Details interface{}
}

func (se *StatusError) Error() string {
	return se.Message
}

func (se *StatusError) Code_() int {
	return se.Code.Code()
}

func (se *StatusError) Equal(err error) bool {
	if statusErr, ok := err.(*StatusError); ok {
		return se.Code.Equal(statusErr.Code)
	}
	return false
}

func (se *StatusError) Details_() interface{} {
	return se.Details
}

type Status struct {
	Code    int
	Message string
	Details interface{}
}

func ToStatus(err error) *Status {
	if err == nil {
		return nil
	}

	if statusErr, ok := err.(*StatusError); ok {
		return &Status{
			Code:    statusErr.Code.Code(),
			Message: statusErr.Message,
			Details: statusErr.Details,
		}
	}

	// If the error is not a StatusError, create a generic Status with code -500 and use the error message as the message.
	return &Status{
		Code:    -500,
		Message: err.Error(),
	}
}

// WithDetails adds additional error details using the Details field of Status.
func (st *Status) WithDetails(details ...proto.Message) error {
	errDetails := make([]*proto.Message, 0, len(details))
	for _, detail := range details {

		errDetails = append(errDetails, &detail)
	}

	stErr := &StatusError{
		Code:    NewError(st.Code),
		Message: st.Message,
		Details: errDetails,
	}
	stWithDetails := status.New(codes.Code(st.Code), stErr.Error())
	return stWithDetails.Err()
}

func NewError(code int) Error {
	return ErrorInfo{Codee: codes.Code(code), Err: errors.New(GetErrorInfo(code))}
}
