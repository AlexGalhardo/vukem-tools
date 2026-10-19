package archive

import (
	"fmt"
	"strings"
)

// The errors this package returns are typed rather than wrapped strings,
// because the tool layer has to classify each one: a cap that was hit and a
// member that cannot be extracted are the input's problem and must never be
// retried, while anything else is transient. Every one of them carries a
// Message, which is the sentence shown to the person, and every message that
// states a limit states it as a number — somebody who hit a cap needs to know
// what it is.

// FormatError reports a file that is not an archive this package reads.
type FormatError struct {
	// Detected is the MIME type the bytes identified as, empty when nothing
	// matched.
	Detected string
}

// Error implements error.
func (e *FormatError) Error() string {
	if e.Detected == "" {
		return "archive: unrecognised file type"
	}

	return "archive: " + e.Detected + " is not an archive this tool reads"
}

// Message is the sentence to show the person.
func (e *FormatError) Message() string {
	return "That file is not a ZIP, TAR, TAR.GZ or GZ archive. Those are the four this tool reads."
}

// LimitError reports a cap that was reached.
type LimitError struct {
	// Limit names the cap, for the log: "entries", "total size".
	Limit string
	// Cap and Actual are the numbers, and Actual is 0 when the archive
	// declared a size rather than reaching one.
	Cap    int64
	Actual int64
	// Unit is how the numbers read in the message: "entries" or "bytes".
	Unit string
}

// Error implements error.
func (e *LimitError) Error() string {
	return fmt.Sprintf("archive: %s cap of %d %s reached (%d)", e.Limit, e.Cap, e.Unit, e.Actual)
}

// Message is the sentence to show the person.
func (e *LimitError) Message() string {
	if e.Unit == "to one" {
		return fmt.Sprintf(
			"That archive expands more than %d times over, which this tool refuses as a "+
				"decompression bomb.", e.Cap)
	}

	if e.Unit == "bytes" {
		return fmt.Sprintf(
			"That archive unpacks to more than %d MB, which is the most this tool will extract.",
			e.Cap>>20)
	}

	return fmt.Sprintf("That archive holds more than %d %s, which is the most this tool will extract.",
		e.Cap, e.Unit)
}

// MemberError reports one member of an archive that cannot be handled, or an
// archive holding nothing worth extracting.
type MemberError struct {
	// Entry is the member's name as the archive spelled it, empty when the
	// fault is the archive as a whole.
	Entry string
	// Reason is what is wrong, for the log.
	Reason string
	// Public is the sentence to show the person.
	Public string
}

// Error implements error.
func (e *MemberError) Error() string {
	if e.Entry == "" {
		return "archive: " + e.Reason
	}

	return fmt.Sprintf("archive: %q: %s", e.Entry, e.Reason)
}

// Message is the sentence to show the person. It names the offending entry,
// truncated, because an archive with fifty members needs to say which one.
func (e *MemberError) Message() string {
	if e.Public != "" {
		return e.Public
	}

	if e.Entry == "" {
		return "That archive cannot be extracted: " + e.Reason + "."
	}

	return fmt.Sprintf("The entry %q cannot be extracted: %s.", truncate(e.Entry), e.Reason)
}

// OptionError reports options that do not describe a possible archive.
type OptionError struct {
	// Field is the option at fault, in the API's spelling.
	Field string
	// Public is the sentence to show the person.
	Public string
}

// Error implements error.
func (e *OptionError) Error() string { return "archive: " + e.Field + ": " + e.Public }

// Message is the sentence to show the person.
func (e *OptionError) Message() string { return e.Public }

// truncate bounds a name taken from an archive before it reaches a message.
// The name is attacker-controlled, and a 60000-character entry is not
// something to pass on to a browser or a log line.
func truncate(s string) string {
	const longest = 60

	if len(s) <= longest {
		return s
	}

	// Cut on a rune boundary, so a truncated name is still valid UTF-8.
	cut := longest
	for cut > 0 && !isStart(s[cut]) {
		cut--
	}

	return strings.ToValidUTF8(s[:cut], "") + "..."
}

// isStart reports whether b begins a UTF-8 rune.
func isStart(b byte) bool { return b&0xC0 != 0x80 }
