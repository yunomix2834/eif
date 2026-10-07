package apperr

type AppError struct {
	Code ErrorCode
	Err  error
}

func (e *AppError) Error() string {
	return e.Code.Message
}

func (e *AppError) Unwrap() error {
	return e.Err
}

func New(
	code ErrorCode,
	err error,
) *AppError {
	return &AppError{
		Code: code,
		Err:  err,
	}
}
