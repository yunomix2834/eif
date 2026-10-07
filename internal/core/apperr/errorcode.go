package apperr

import "net/http"

type ErrorCode struct {
	Code       string
	StatusCode int
	Message    string
}

var (
	CodeInternalError = ErrorCode{
		Code:       "EIF-SYS-500",
		StatusCode: http.StatusInternalServerError,
		Message:    "Internal error",
	}

	CodeInvalidRequest = ErrorCode{
		Code:       "EIF-REQ-400",
		StatusCode: http.StatusBadRequest,
		Message:    "Invalid request",
	}

	CodeUnauthorized = ErrorCode{
		Code:       "EIF-AUTH-401",
		StatusCode: http.StatusUnauthorized,
		Message:    "Unauthorized",
	}

	CodeSessionExpired = ErrorCode{
		Code:       "EIF-AUTH-SESSION-401",
		StatusCode: http.StatusUnauthorized,
		Message:    "Session is missing or expired",
	}

	CodeHDDTGDTAuthenticationFailed = ErrorCode{
		Code:       "EIF-HDDT-GDT-AUTH-401",
		StatusCode: http.StatusUnauthorized,
		Message:    "HDDT GDT authentication failed",
	}

	CodeHDDTGDTCaptchaExpired = ErrorCode{
		Code:       "EIF-HDDT-GDT-CAPTCHA-401",
		StatusCode: http.StatusUnauthorized,
		Message:    "HDDT GDT captcha session is missing or expired",
	}

	CodeHDDTGDTProfileFailed = ErrorCode{
		Code:       "EIF-HDDT-GDT-PROFILE-502",
		StatusCode: http.StatusBadGateway,
		Message:    "HDDT GDT profile initialization failed",
	}

	CodeHDDTGDTRefreshFailed = ErrorCode{
		Code:       "EIF-HDDT-GDT-REFRESH-401",
		StatusCode: http.StatusUnauthorized,
		Message:    "HDDT GDT session refresh failed",
	}

	CodeHDDTGDTTimeout = ErrorCode{
		Code:       "EIF-HDDT-GDT-504",
		StatusCode: http.StatusGatewayTimeout,
		Message:    "HDDT GDT timeout",
	}

	CodeHDDTGDTBadGateway = ErrorCode{
		Code:       "EIF-HDDT-GDT-502",
		StatusCode: http.StatusBadGateway,
		Message:    "HDDT GDT request failed",
	}

	CodeHDDTGDTRateLimited = ErrorCode{
		Code:       "EIF-HDDT-GDT-RATE-LIMIT-429",
		StatusCode: http.StatusTooManyRequests,
		Message:    "HDDT GDT rate limit exceeded",
	}

	CodeHDDTGDTInvalidResponse = ErrorCode{
		Code:       "EIF-HDDT-GDT-INVALID-502",
		StatusCode: http.StatusBadGateway,
		Message:    "HDDT GDT returned an invalid response",
	}

	CodeUpdateUnavailable = ErrorCode{
		Code:       "EIF-UPDATE-409",
		StatusCode: http.StatusConflict,
		Message:    "No newer EIF version is available",
	}

	CodeUpdateInProgress = ErrorCode{
		Code:       "EIF-UPDATE-IN-PROGRESS-409",
		StatusCode: http.StatusConflict,
		Message:    "An EIF update is already in progress",
	}

	CodeUpdateFailed = ErrorCode{
		Code:       "EIF-UPDATE-502",
		StatusCode: http.StatusBadGateway,
		Message:    "EIF could not download or prepare the update",
	}
)
