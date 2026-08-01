// Package voxerr carries the closed set of machine-branchable failures. Every
// error that reaches the user is rendered as one YAML document on stderr, so an
// agent branches on `code` instead of parsing prose.
package voxerr

import "fmt"

// Codes. Adding one is a surface change — it belongs in docs/cli-schema.md too.
const (
	NotAuthenticated   = "not_authenticated"
	AudioTooLarge      = "audio_too_large"
	AudioUnsupported   = "audio_unsupported"
	VocabNotFound      = "vocab_not_found"
	VocabQuotaExceeded = "vocab_quota_exceeded"
	VocabModelMismatch = "vocab_model_mismatch"
	SessionNotFound    = "session_not_found"
	SessionAmbiguous   = "session_ambiguous"
	APIError           = "api_error"
)

// Exit codes.
const (
	ExitUsage    = 1
	ExitAPI      = 2
	ExitNotFound = 3
)

type Error struct {
	Code    string `yaml:"code"`
	Message string `yaml:"message"`
	Hint    string `yaml:"hint,omitempty"`
}

func (e *Error) Error() string { return e.Message }

func (e *Error) ExitCode() int {
	switch e.Code {
	case APIError:
		return ExitAPI
	case SessionNotFound, VocabNotFound:
		return ExitNotFound
	default:
		return ExitUsage
	}
}

func New(code, format string, a ...any) *Error {
	return &Error{Code: code, Message: fmt.Sprintf(format, a...)}
}

// WithHint attaches the command that resolves the failure.
func (e *Error) WithHint(format string, a ...any) *Error {
	e.Hint = fmt.Sprintf(format, a...)
	return e
}
