package toolkit

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/AlexGalhardo/vukem-tools/clients/apiclient"
)

// Codes an agent can branch on. They are part of the CLI and MCP contract:
// add one, never rename one.
const (
	CodeInvalidInput   = "invalid_input"
	CodeMissingKey     = "missing_key"
	CodeUnauthorized   = "unauthorized"
	CodeMissingAbility = "missing_ability"
	CodeFileTooLarge   = "file_too_large"
	CodeNotFound       = "not_found"
	CodeUploadFailed   = "upload_failed"
	CodeResultExpired  = "result_expired"
	CodeRateLimited    = "rate_limited"
	CodeConversion     = "conversion_failed"
	CodeCancelled      = "conversion_cancelled"
	CodeTimeout        = "timeout"
	CodeServer         = "server_error"
	CodeNetwork        = "network_error"
	CodeIO             = "io_error"
	CodeBrowserOnly    = "browser_only"
	CodeUnknownTool    = "unknown_tool"
)

// Error is a failure with a stable code and a sentence an agent can act on.
// It never carries the API key.
type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	// ConversionType is the server's own code for a failed conversion.
	ConversionType string `json:"conversion_type,omitempty"`
	// ConversionID is set when the failure came after a conversion started.
	ConversionID string `json:"conversion_id,omitempty"`
	// Retry is true when sending the same input again could succeed.
	Retry bool `json:"retry,omitempty"`
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

func invalid(msg string) *Error { return &Error{Code: CodeInvalidInput, Message: msg} }

func asError(err error) *Error {
	if te, ok := errors.AsType[*Error](err); ok {
		return te
	}

	return &Error{Code: CodeServer, Message: err.Error()}
}

// AsError returns err as an *Error, wrapping an unexpected error as
// server_error so every failure reaches the caller with a code.
func AsError(err error) *Error { return asError(err) }

// FromStatus maps a refused API response to a coded error.
func FromStatus(status int, body []byte) *Error { return fromResponse(status, body) }

// fromResponse maps a refused API response to a coded error.
func fromResponse(status int, body []byte) *Error {
	var problem apiclient.Problem

	if err := json.Unmarshal(body, &problem); err != nil {
		// The upload endpoint answers a refusal as plain text, and that
		// sentence is the only explanation the person gets.
		if text := plainSentence(body); text != "" {
			problem = apiclient.Problem{Detail: text}
		}
	}

	return fromProblem(status, problem)
}

func fromProblem(status int, problem apiclient.Problem) *Error {
	detail := strings.TrimSpace(problem.Detail)
	if detail == "" {
		detail = strings.TrimSpace(problem.Title)
	}

	if detail == "" {
		detail = http.StatusText(status)
	}

	var issues []string

	for _, issue := range problem.Issues {
		if issue.Message != "" {
			issues = append(issues, issue.Path+": "+issue.Message)
		}
	}

	if len(issues) > 0 {
		detail += " (" + strings.Join(issues, "; ") + ")"
	}

	switch status {
	case http.StatusUnauthorized:
		return &Error{
			Code:    CodeUnauthorized,
			Message: "The API key was refused. Check VUKEM_TOOLS_API_KEY: it may be wrong, revoked or expired.",
		}
	case http.StatusForbidden:
		return &Error{
			Code: CodeMissingAbility,
			Message: "The API key may not run this tool: " + strings.TrimRight(detail, ".") +
				". Issue a key with the tool's ability.",
		}
	case http.StatusNotFound:
		return &Error{Code: CodeNotFound, Message: detail}
	case http.StatusGone:
		return &Error{Code: CodeResultExpired, Message: detail, Retry: true}
	case http.StatusRequestEntityTooLarge:
		return &Error{Code: CodeFileTooLarge, Message: detail}
	case http.StatusUnprocessableEntity, http.StatusBadRequest:
		return &Error{Code: CodeInvalidInput, Message: detail}
	case http.StatusTooManyRequests:
		return &Error{
			Code:    CodeRateLimited,
			Message: strings.TrimRight(detail, ".") + ". Wait a moment and try again.",
			Retry:   true,
		}
	}

	if status >= http.StatusInternalServerError {
		return &Error{Code: CodeServer, Message: detail, Retry: true}
	}

	return &Error{Code: CodeServer, Message: detail}
}

// plainSentence returns body as one short printable line, or "" when it is
// not that (HTML, binary, or too long to be a sentence).
func plainSentence(body []byte) string {
	text := strings.TrimSpace(string(body))
	if text == "" || len(text) > 300 || strings.ContainsAny(text, "<>{}\x00") || strings.Contains(text, "\n") {
		return ""
	}

	for _, r := range text {
		if r < ' ' || r == 0x7f {
			return ""
		}
	}

	return text
}
