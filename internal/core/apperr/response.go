package apperr

import "errors"

type ErrorResponse struct {
	StatusCode int    `json:"status_code"`
	Code       string `json:"code"`
	Message    string `json:"message"`
	RequestID  string `json:"request_id,omitempty"`
}

func BuildResponse(
	err error,
	requestID string,
) ErrorResponse {
	var appErr *AppError
	if errors.As(
		err,
		&appErr,
	) {
		return ErrorResponse{
			StatusCode: appErr.Code.StatusCode,
			Code:       appErr.Code.Code,
			Message:    appErr.Code.Message,
			RequestID:  requestID,
		}
	}

	return ErrorResponse{
		StatusCode: CodeInternalError.StatusCode,
		Code:       CodeInternalError.Code,
		Message:    CodeInternalError.Message,
		RequestID:  requestID,
	}
}
