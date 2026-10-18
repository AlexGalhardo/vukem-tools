package pdf

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// NotPDFError reports a file that is not a PDF at all.
//
// It is a separate type from "the engine rejected it" because the two deserve
// different answers: a file with no PDF header was never a PDF, and telling
// somebody "the converter could not process that file" when they uploaded a
// Word document sends them looking at the wrong thing.
type NotPDFError struct {
	// Reason says what was wrong with the header, for the owner-only detail.
	Reason string
}

// Error implements error.
func (e *NotPDFError) Error() string { return "pdf: not a PDF document: " + e.Reason }

// Message is the sentence to show whoever uploaded the file.
func (*NotPDFError) Message() string {
	return "That file is not a PDF document. It has no PDF header."
}

// ErrEncrypted means the document is password-protected.
//
// Every operation but Unlock refuses one. Running an engine against an
// encrypted document produces either an error or, worse with some engines, an
// empty document — and an empty document that opens is the failure mode a user
// does not notice until it matters.
var ErrEncrypted = errors.New("pdf: the document is encrypted")

// ErrWrongPassword means the password supplied does not open the document.
//
// It deliberately carries nothing else. Whether the document had a user
// password, an owner password or both is not reported anywhere, because that
// is information about somebody else's document that the person holding a
// wrong password has not earned.
var ErrWrongPassword = errors.New("pdf: the password does not open this document")

// ErrNotEncrypted means Unlock was given a document that has no password.
var ErrNotEncrypted = errors.New("pdf: the document is not encrypted")

// DamagedError reports a document whose structure could not be read.
type DamagedError struct {
	// Detail is the engine's own account, for the owner-only detail.
	Detail string
}

// Error implements error.
func (e *DamagedError) Error() string { return "pdf: the document is damaged: " + e.Detail }

// Message is the sentence to show whoever uploaded the file.
func (*DamagedError) Message() string {
	return "That PDF is damaged badly enough that its structure cannot be read. " +
		"Try the repair tool on it first."
}

// TooManyPagesError reports a document past the page cap.
//
// There is a cap because several operations hold one item per page in memory —
// a page range, a stamp, a structural edit — and a document with a million
// pages would otherwise turn a bounded operation into an unbounded one.
type TooManyPagesError struct {
	Pages int
	Limit int
}

// Error implements error.
func (e *TooManyPagesError) Error() string {
	return fmt.Sprintf("pdf: %d pages, the limit is %d", e.Pages, e.Limit)
}

// Message is the sentence to show whoever uploaded the file.
func (e *TooManyPagesError) Message() string {
	return "That PDF has " + strconv.Itoa(e.Pages) + " pages. The limit for this tool is " +
		strconv.Itoa(e.Limit) + "."
}

// PageRangeError reports a page range that cannot be applied to this
// document: bad syntax, or a page the document does not have.
//
// A page the document does not have is an error rather than a page quietly
// skipped. Somebody asking for pages 1-20 of a 10-page document has made a
// mistake, and a tool that silently returns 10 pages has hidden it.
type PageRangeError struct {
	// Input is the range as it was written.
	Input string
	// Reason says what is wrong with it, written for the user.
	Reason string
}

// Error implements error.
func (e *PageRangeError) Error() string {
	return fmt.Sprintf("pdf: page range %q: %s", e.Input, e.Reason)
}

// Message is the sentence to show whoever submitted the range.
func (e *PageRangeError) Message() string { return e.Reason }

// OptionError reports an option value this package cannot act on.
type OptionError struct {
	// Field is the option's name as the API spells it, e.g. "dpi".
	Field string
	// Reason is written for the user.
	Reason string
}

// Error implements error.
func (e *OptionError) Error() string { return "pdf: option " + e.Field + ": " + e.Reason }

// Message is the sentence to show whoever submitted the option.
func (e *OptionError) Message() string { return e.Reason }

// EngineMissingError reports an operation whose engine is not installed.
//
// It is an internal failure, not the input's fault: the tool should not have
// been on offer. app/tools hides a tool whose engine is absent, and this is
// what catches the case where that gate was wrong.
type EngineMissingError struct {
	Engine string
}

// Error implements error.
func (e *EngineMissingError) Error() string { return "pdf: engine " + e.Engine + " is not installed" }

// EngineError reports an engine that ran and failed.
//
// Stderr is kept on the error rather than put in the message, because an
// engine's error text routinely contains the working directory and the whole
// command line: it belongs in the owner-only detail and nowhere else.
type EngineError struct {
	Engine string
	Code   int
	Stderr string
}

// Error implements error.
func (e *EngineError) Error() string {
	if e.Stderr == "" {
		return fmt.Sprintf("pdf: %s exited %d", e.Engine, e.Code)
	}

	return fmt.Sprintf("pdf: %s exited %d: %s", e.Engine, e.Code, firstLine(e.Stderr))
}

// TimeoutError reports an engine still running at its deadline.
type TimeoutError struct {
	Engine string
}

// Error implements error.
func (e *TimeoutError) Error() string { return "pdf: " + e.Engine + " exceeded its time limit" }

// firstLine is the part of an engine's error worth putting in a message.
func firstLine(s string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(s), "\n")

	return strings.TrimSpace(line)
}

// qpdf's exit codes, which are a documented contract rather than a
// convention: 0 success, 2 an error that produced no usable output, 3
// warnings whose output is still usable.
const (
	qpdfOK       = 0
	qpdfError    = 2
	qpdfWarnings = 3
)

// usableExit reports whether a qpdf invocation produced output worth keeping.
// Warnings are usable on purpose: a document qpdf had to reconstruct is the
// whole point of the repair tool, and refusing it would make repair impossible.
func usableExit(code int) bool { return code == qpdfOK || code == qpdfWarnings }
