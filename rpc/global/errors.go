package global

// import (
// 	"strconv"

// 	"github.com/golang/protobuf/proto"
// 	epb "google.golang.org/genproto/googleapis/rpc/errdetails"
// 	"google.golang.org/grpc/codes"
// 	"google.golang.org/grpc/status"
// )

// func (e customError) Code() int {
// 	return int(e.code)
// }

// func (e customError) Equal(err error) bool {
// 	if errCode, ok := err.(Error); ok {
// 		return e.Code() == errCode.Code()
// 	}
// 	return false
// }

// // customError is a custom error type that implements the error interface.
// type customError struct {
// 	code    codes.Code
// 	message string
// 	details []proto.Message
// }

// // Error returns the error message.
// func (ce *customError) Error() string {
// 	return ce.message
// }

// // GRPCCode returns the gRPC error code.
// func (ce *customError) GRPCCode() codes.Code {
// 	return ce.code
// }

// // Is checks if the given error is equal to the current error.
// func (ce *customError) Is(err error) bool {
// 	if grpcErr, ok := err.(interface{ GRPCCode() codes.Code }); ok {
// 		return grpcErr.GRPCCode() == ce.code
// 	}
// 	return false
// }

// // Code converts error to ecode.
// func Code(e error) Error {
// 	if e == nil {
// 		return OK
// 	}

// 	if errCode, ok := e.(Error); ok {
// 		return errCode
// 	}

// 	i, err := strconv.Atoi(e.Error())
// 	if err != nil {
// 		i = -500
// 	}
// 	return ecode(i)
// }

// // Int converts int to ecode.
// func Int(i int) Error {
// 	return ecode(i)
// }

// // WithBadRequestFieldViolation adds a BadRequest_FieldViolation detail to the error.
// func WithBadRequestFieldViolation(err error, field, description string) error {
// 	violation := &epb.BadRequest_FieldViolation{
// 		Field:       field,
// 		Description: description,
// 	}

// 	return WithDetails(err, violation)
// }

// // ToError converts a Status to an error.
// func ToError(st *status.Status) error {
// 	if st == nil {
// 		return nil
// 	}

// 	return &customError{
// 		code:    st.Code(),
// 		message: st.Message(),
// 	}
// }

// // ToError2 converts a Status to an error.
// func ToError2(st *status.Status) error {
// 	if st == nil {
// 		return nil
// 	}

// 	return st.Err()
// }

// // WithDetails adds additional error details using the details field.
// func WithDetails(err error, details ...proto.Message) error {
// 	if err == nil {
// 		return nil
// 	}

// 	st, ok := status.FromError(err)
// 	if !ok {
// 		return err
// 	}

// 	errDetails := make([]proto.Message, 0, len(details))
// 	for _, detail := range details {
// 		errDetails = append(errDetails, detail)
// 	}

// 	st, _ = st.WithDetails(errDetails...)
// 	return st.Err()
// }

// // WithUnauthorized adds an Unauthorized error detail to the error.
// func WithUnauthorized(err error, realm string) error {
// 	detail := &epb.ErrorInfo{
// 		Reason:   "Unauthorized",
// 		Metadata: map[string]string{"realm": realm},
// 	}

// 	st, ok := status.FromError(err)
// 	if !ok {
// 		return WithDetails(err, detail)
// 	}

// 	st, _ = st.WithDetails(detail)
// 	return st.Err()
// }

// // WithForbidden adds a Forbidden error detail to the error.
// func WithForbidden(err error, reason string) error {
// 	detail := &epb.ErrorInfo{
// 		Reason:   "Forbidden",
// 		Metadata: map[string]string{"reason": reason},
// 	}

// 	st, ok := status.FromError(err)
// 	if !ok {
// 		return WithDetails(err, detail)
// 	}

// 	st, _ = st.WithDetails(detail)
// 	return st.Err()
// }

// // WithTooManyRequests adds a TooManyRequests error detail to the error.
// func WithTooManyRequests(err error, limit, retryAfterSeconds int) error {
// 	violation := &epb.QuotaFailure_Violation{
// 		Subject:     "*",
// 		Description: "Too Many Requests",
// 	}

// 	if limit > 0 {
// 		violation.Description += ", limit: " + strconv.Itoa(limit)
// 	}

// 	detail := &epb.QuotaFailure{
// 		Violations: []*epb.QuotaFailure_Violation{violation},
// 	}

// 	if retryAfterSeconds > 0 {
// 		detail.Violations[0].Description += ", retry after " + strconv.Itoa(retryAfterSeconds) + "s"
// 	}

// 	st, ok := status.FromError(err)
// 	if !ok {
// 		return WithDetails(err, detail)
// 	}

// 	st, _ = st.WithDetails(detail)
// 	return st.Err()
// }

// // WithInternalServerError adds an InternalServerError error detail to the error.
// func WithInternalServerError(err error) error {
// 	detail := &epb.ErrorInfo{
// 		Reason: "Internal Server Error",
// 	}

// 	st, ok := status.FromError(err)
// 	if !ok {
// 		return WithDetails(err, detail)
// 	}

// 	st, _ = st.WithDetails(detail)
// 	return st.Err()
// }

// // WithServiceUnavailable adds a ServiceUnavailable error detail to the error.
// func WithServiceUnavailable(err error, retryAfterSeconds int) error {
// 	detail := &epb.ErrorInfo{
// 		Reason: "Service Unavailable",
// 	}

// 	if retryAfterSeconds > 0 {
// 		detail.Metadata = map[string]string{
// 			"retryAfterSeconds": strconv.Itoa(retryAfterSeconds),
// 		}
// 	}

// 	st, ok := status.FromError(err)
// 	if !ok {
// 		return WithDetails(err, detail)
// 	}

// 	st, _ = st.WithDetails(detail)
// 	return st.Err()
// }
