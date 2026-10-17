package transcription

// Failure carries a stable, public-safe reason. Never put subprocess output,
// credentials, or signed media URLs in its message.
type Failure struct {
	Code    string
	Message string
}

func (e *Failure) Error() string { return e.Message }
func (e *Failure) Unwrap() error { return ErrUnavailable }

func Failed(code, message string) error { return &Failure{Code: code, Message: message} }
