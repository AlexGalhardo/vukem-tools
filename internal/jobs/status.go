// Package jobs is the conversion pipeline: the state of a conversion, its
// error taxonomy, the queue job that runs it and the progress it reports.
//
// A conversion is the domain object the user sees. The queue message is only
// the trigger that makes a worker pick it up, so the two are deliberately
// separate: a message can be delivered twice, or be lost and reclaimed, and
// the conversion row still has exactly one state.
package jobs

import (
	"context"
	"errors"
	"fmt"
	"net/http"
)

// Status is where a conversion is in its life.
type Status string

// The states a conversion can be in. A conversion starts pending, runs at
// most once at a time, and ends finished, error or cancelled. The three end
// states are final: nothing moves a conversion out of them.
const (
	// Pending means queued and not yet picked up, or released back after an
	// interrupted attempt.
	Pending Status = "pending"
	// Running means a worker is on it.
	Running Status = "running"
	// Finished means the result is stored and downloadable until it expires.
	Finished Status = "finished"
	// Errored means it failed; ErrorType says how.
	Errored Status = "error"
	// Cancelled means the owner stopped it.
	Cancelled Status = "cancelled"
)

// Final reports whether the status is an end state.
func (s Status) Final() bool {
	switch s {
	case Finished, Errored, Cancelled:
		return true
	case Pending, Running:
		return false
	default:
		return false
	}
}

// Valid reports whether s is one of the five states.
func (s Status) Valid() bool {
	switch s {
	case Pending, Running, Finished, Errored, Cancelled:
		return true
	default:
		return false
	}
}

// ErrorType is a stable code from the error taxonomy. It is the same string
// in the database, in the API, in the OpenAPI document and in the interface:
// clients branch on it, so it is part of the public contract and renaming
// one is a breaking change.
type ErrorType string

// The whole taxonomy. There is no "other": an unclassified failure is
// Internal, which is also the only one whose message says nothing about the
// input, because an unclassified failure may be a bug and its detail may
// contain paths or arguments.
const (
	// OutOfMemory means the engine asked for more memory than its limit.
	OutOfMemory ErrorType = "out_of_memory"
	// DiskFull means the working directory ran out of space.
	DiskFull ErrorType = "disk_full"
	// UnsupportedInput means the file is not a format this tool reads.
	UnsupportedInput ErrorType = "unsupported_input"
	// InputTooLarge means the file is past a documented limit (bytes,
	// pixels, duration or page count).
	InputTooLarge ErrorType = "input_too_large"
	// EngineFailure means the engine ran and rejected the job.
	EngineFailure ErrorType = "engine_failure"
	// Timeout means the engine was still running at its deadline.
	Timeout ErrorType = "timeout"
	// CancelledByUser means the owner stopped it.
	CancelledByUser ErrorType = "cancelled"
	// QuotaExceeded means the account spent its daily budget.
	QuotaExceeded ErrorType = "quota_exceeded"
	// Infected means the malware scanner reported a signature in the file.
	Infected ErrorType = "infected"
	// Internal means something failed that is not the input's fault.
	Internal ErrorType = "internal"
)

// Info describes one error type for every surface that shows it.
type Info struct {
	// Type is the stable code.
	Type ErrorType
	// Message is safe to show to anyone: it never names a path, an engine
	// argument or an internal limit the client cannot act on.
	Message string
	// Status is the HTTP status a synchronous refusal uses. An asynchronous
	// failure is reported inside a 200 response on the conversion itself.
	Status int
	// Retry says whether running the same input again could succeed. It
	// decides whether the worker returns the error to the queue (which
	// retries with backoff) or settles the conversion at once.
	Retry bool
}

// catalog is the taxonomy, in the order the documentation lists it.
var catalog = []Info{
	{UnsupportedInput, "That file is not a format this tool can read.", http.StatusUnsupportedMediaType, false},
	// A dedicated code, not folded into unsupported_input: the user needs to
	// know their file was refused because it is dangerous, not because it is
	// the wrong format, and an operator needs to be able to count these.
	// The message says nothing about the signature: naming it tells an
	// attacker which scanner and which definitions are in use.
	{Infected, "That file was rejected by the malware scanner.", http.StatusUnprocessableEntity, false},
	{InputTooLarge, "That file is past the limit for this tool.", http.StatusRequestEntityTooLarge, false},
	{QuotaExceeded, "You have used today's conversion budget.", http.StatusTooManyRequests, false},
	{CancelledByUser, "The conversion was cancelled.", http.StatusConflict, false},
	{EngineFailure, "The converter could not process that file.", http.StatusUnprocessableEntity, false},
	{Timeout, "The conversion took too long and was stopped.", http.StatusGatewayTimeout, true},
	{OutOfMemory, "The conversion needed more memory than it is allowed.", http.StatusInsufficientStorage, true},
	{DiskFull, "The server is out of working space. Try again shortly.", http.StatusInsufficientStorage, true},
	{Internal, "Something went wrong on our side.", http.StatusInternalServerError, true},
}

// byType indexes catalog. Built once at load, so a type added to the catalog
// is reachable without touching a second map.
var byType = func() map[ErrorType]Info {
	m := make(map[ErrorType]Info, len(catalog))
	for _, i := range catalog {
		m[i.Type] = i
	}

	return m
}()

// Catalog returns the taxonomy in documentation order. The slice is a copy,
// so a caller cannot reorder the taxonomy for everyone else.
func Catalog() []Info { return append([]Info(nil), catalog...) }

// Describe returns the entry for t, falling back to Internal for a code that
// is not in the taxonomy. It never fails: a row stored by an older version
// must still render.
func Describe(t ErrorType) Info {
	if i, ok := byType[t]; ok {
		return i
	}

	return byType[Internal]
}

// Known reports whether t is in the taxonomy.
func Known(t ErrorType) bool {
	_, ok := byType[t]
	return ok
}

// ConversionError is a conversion failure classified by the taxonomy.
//
// Detail is for the owner: it goes to the log and to the conversion's
// error_detail column, and is never part of an API response, because it may
// hold a path, an engine command line or an engine's own message.
type ConversionError struct {
	Type   ErrorType
	Detail string
	Err    error
}

// Fail returns a ConversionError of type t wrapping err. detail is owner-only.
func Fail(t ErrorType, err error, detail string) *ConversionError {
	return &ConversionError{Type: t, Detail: detail, Err: err}
}

// Failf returns a ConversionError of type t whose detail is formatted, with no
// wrapped error.
func Failf(t ErrorType, format string, args ...any) *ConversionError {
	return &ConversionError{Type: t, Detail: fmt.Sprintf(format, args...)}
}

// Error implements error. It reports the code and the owner-only detail, so
// a logged failure says which kind it was; never return it to a client.
func (f *ConversionError) Error() string {
	switch {
	case f.Detail != "" && f.Err != nil:
		return string(f.Type) + ": " + f.Detail + ": " + f.Err.Error()
	case f.Detail != "":
		return string(f.Type) + ": " + f.Detail
	case f.Err != nil:
		return string(f.Type) + ": " + f.Err.Error()
	default:
		return string(f.Type)
	}
}

// Unwrap implements errors.Unwrap.
func (f *ConversionError) Unwrap() error { return f.Err }

// Classify maps any error to an error type.
//
// A cancelled context is the owner stopping the conversion or the worker
// shutting down; a deadline is the engine running past its timeout. Anything
// not classified is Internal, which is retried: a failure nobody labelled is
// more likely a transient fault than a bad file, and labelling it as the
// user's fault would be a lie.
func Classify(err error) ErrorType {
	if err == nil {
		return ""
	}

	if f, ok := errors.AsType[*ConversionError](err); ok && Known(f.Type) {
		return f.Type
	}

	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return Timeout
	case errors.Is(err, context.Canceled):
		return CancelledByUser
	default:
		return Internal
	}
}

// Retryable reports whether err is worth another attempt.
func Retryable(err error) bool { return Describe(Classify(err)).Retry }

// DetailOf returns the owner-only detail of err, or its message when err is
// not a ConversionError. Use it for the error_detail column and the log, never
// for a
// response body.
func DetailOf(err error) string {
	if err == nil {
		return ""
	}

	if f, ok := errors.AsType[*ConversionError](err); ok {
		return f.Error()
	}

	return err.Error()
}
