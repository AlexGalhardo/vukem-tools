package pdf

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

// secureTimeout bounds an encryption or decryption. Both rewrite the object
// graph once, so they cost about what a copy costs.
const secureTimeout = 10 * time.Minute

// Printing is what a protected document allows a reader to print.
type Printing string

// The printing permissions, in the order the interface offers them.
const (
	// PrintFull allows printing at full resolution.
	PrintFull Printing = "full"
	// PrintLow allows printing, degraded, which is the flag intended to stop
	// a printed copy being rescanned into a clean one.
	PrintLow Printing = "low"
	// PrintNone forbids printing.
	PrintNone Printing = "none"
)

// Valid reports whether p is a permission qpdf accepts.
func (p Printing) Valid() bool { return p == PrintFull || p == PrintLow || p == PrintNone }

// Modification is what a protected document allows a reader to change.
type Modification string

// The modification permissions, from most to least.
const (
	// ModifyAll allows any change.
	ModifyAll Modification = "all"
	// ModifyAnnotate allows comments and form filling.
	ModifyAnnotate Modification = "annotate"
	// ModifyForm allows form filling and signing only.
	ModifyForm Modification = "form"
	// ModifyAssembly allows inserting, rotating and deleting pages only.
	ModifyAssembly Modification = "assembly"
	// ModifyNone forbids changes.
	ModifyNone Modification = "none"
)

// modifications is the closed set, in the order the interface offers them.
var modifications = []Modification{ModifyAll, ModifyAnnotate, ModifyForm, ModifyAssembly, ModifyNone}

// Valid reports whether m is a permission qpdf accepts.
func (m Modification) Valid() bool { return slices.Contains(modifications, m) }

// Modifications returns the modification permissions, for the documentation
// and the interface.
func Modifications() []string {
	out := make([]string, 0, len(modifications))
	for _, m := range modifications {
		out = append(out, string(m))
	}

	return out
}

// Printings returns the printing permissions, for the documentation and the
// interface.
func Printings() []string { return []string{string(PrintFull), string(PrintLow), string(PrintNone)} }

// MaxPasswordBytes caps a password.
//
// AES-256 hashes the password, so length costs nothing cryptographically; the
// cap is there because the password travels through an argument file and a
// pipe, and an unbounded field is an unbounded write.
const MaxPasswordBytes = 256

// ProtectRequest is one encryption.
type ProtectRequest struct {
	Dir    string
	Input  string
	Output string
	// UserPassword opens the document. Empty means the document opens
	// without one and only the permissions are enforced, which is what
	// "restrict what readers may do" means.
	UserPassword string
	// OwnerPassword lifts the restrictions. It is required: a document with
	// permissions and no owner password is a document any tool can strip,
	// including this one.
	OwnerPassword string
	// Print and Modify are the permissions. Empty means full.
	Print  Printing
	Modify Modification
	// AllowExtract allows copying text out. Accessibility extraction is
	// always allowed and is not an option: a screen reader is not a threat
	// model, and the PDF 2.0 specification deprecated the flag that would
	// forbid it.
	AllowExtract bool
}

// Protect encrypts a document with AES-256 and the permissions asked for.
//
// Every password reaches qpdf through an argument file rather than the
// command line. That is the whole reason the function is shaped this way: an
// argument in argv is readable from /proc by anything running as the same
// user, and "the conversion service leaked the password its user just chose"
// is not a failure worth risking to save writing a temporary file. The file
// is created 0600 inside the job directory and removed as soon as qpdf exits,
// and the job directory is deleted when the conversion ends in any case.
func (e *Engines) Protect(ctx context.Context, req ProtectRequest) error {
	if err := req.validate(); err != nil {
		return err
	}

	if _, err := e.Inspect(ctx, req.Dir, req.Input); err != nil {
		return err
	}

	args := []string{
		"--encrypt",
		"--user-password=" + req.UserPassword,
		"--owner-password=" + req.OwnerPassword,
		"--bits=256",
		"--print=" + string(req.printing()),
		"--modify=" + string(req.modification()),
		"--extract=" + yesNo(req.AllowExtract),
		// Accessibility is always allowed: PDF 2.0 deprecated the flag that
		// forbids it, and a document a screen reader cannot read is not a
		// security win.
		"--accessibility=y",
		"--",
		req.Input,
		req.Output,
	}

	file, err := writeArgFile(req.Dir, args)
	if err != nil {
		return err
	}

	defer func() { _ = os.Remove(file) }()

	return e.qpdf(ctx, req.Dir, []string{"@" + file})
}

// validate checks a protect request before anything runs.
func (r ProtectRequest) validate() error {
	switch {
	case r.OwnerPassword == "":
		return &OptionError{
			Field: "owner_password",
			Reason: "Set an owner password. Without one the restrictions can be removed by any " +
				"PDF tool, including this one, so they would not be restrictions.",
		}
	case len(r.OwnerPassword) > MaxPasswordBytes || len(r.UserPassword) > MaxPasswordBytes:
		return &OptionError{
			Field:  "owner_password",
			Reason: "A password may be at most " + strconv.Itoa(MaxPasswordBytes) + " characters.",
		}
	case r.UserPassword != "" && r.UserPassword == r.OwnerPassword:
		return &OptionError{
			Field: "owner_password",
			Reason: "The owner password must differ from the one that opens the document, " +
				"or anybody who can open it can also lift the restrictions.",
		}
	case r.Print != "" && !r.Print.Valid():
		return &OptionError{Field: "print", Reason: "Printing is one of: " + strings.Join(Printings(), ", ") + "."}
	case r.Modify != "" && !r.Modify.Valid():
		return &OptionError{
			Field:  "modify",
			Reason: "Modification is one of: " + strings.Join(Modifications(), ", ") + ".",
		}
	default:
		return nil
	}
}

// printing fills in the default.
func (r ProtectRequest) printing() Printing {
	if r.Print == "" {
		return PrintFull
	}

	return r.Print
}

// modification fills in the default.
func (r ProtectRequest) modification() Modification {
	if r.Modify == "" {
		return ModifyAll
	}

	return r.Modify
}

// UnlockRequest is one password removal.
type UnlockRequest struct {
	Dir    string
	Input  string
	Output string
	// Password is the one the user supplied. There is no other source for it
	// and no fallback: see Unlock.
	Password string
}

// Unlock removes a password the user supplied.
//
// Three things it deliberately does not do, because each one is a decision
// rather than an omission:
//
// It makes exactly one attempt. The password it is given is tried once, and a
// failure is returned. There is no retry, no list of common passwords and no
// empty-password fallback — so there is no code path in this package that
// could be turned into a brute-force attack by changing a loop bound, which
// ROADMAP S6.5 asks to be provable by a test.
//
// It does not say which password was missing. qpdf can report whether a
// document needed a user password, an owner password or both, and none of
// that reaches the caller: a wrong password gets ErrWrongPassword and nothing
// else. Telling somebody holding a wrong password that the file "only has an
// owner password" is telling them the restrictions are cosmetic, about a
// document that is not theirs.
//
// It passes the password on standard input rather than in an argument, for
// the reason Protect's comment gives.
func (e *Engines) Unlock(ctx context.Context, req UnlockRequest) error {
	if req.Password == "" {
		return &OptionError{Field: "password", Reason: "Enter the password that opens the document."}
	}

	if len(req.Password) > MaxPasswordBytes {
		return &OptionError{
			Field:  "password",
			Reason: "A password may be at most " + strconv.Itoa(MaxPasswordBytes) + " characters.",
		}
	}

	if err := ValidateHeader(req.Input); err != nil {
		return err
	}

	encrypted, err := e.IsEncrypted(ctx, req.Dir, req.Input)
	if err != nil {
		return err
	}

	if !encrypted {
		return ErrNotEncrypted
	}

	// One attempt. The password goes down the pipe once; whatever qpdf makes
	// of it is the answer.
	_, runErr := e.run(ctx, command{
		engine:  EngineQPDF,
		args:    []string{"--password-file=-", "--decrypt", req.Input, req.Output},
		dir:     req.Dir,
		stdin:   strings.NewReader(req.Password),
		timeout: secureTimeout,
	})
	if runErr == nil {
		return nil
	}

	engineErr, ok := errors.AsType[*EngineError](runErr)
	if !ok {
		return runErr
	}

	if usableExit(engineErr.Code) {
		return nil
	}

	// qpdf answers a wrong password with exit 2 and "invalid password". The
	// same exit code also means an unreadable document, which is why the
	// message is checked: the two need different answers, and a document that
	// is simply damaged should not be reported as a password problem. The
	// engine's own text goes nowhere near the response.
	if strings.Contains(strings.ToLower(engineErr.Stderr), "invalid password") {
		// The output may be a partial file qpdf began before giving up.
		removeAll([]string{req.Output})

		return ErrWrongPassword
	}

	return &DamagedError{Detail: engineErr.Error()}
}

// writeArgFile writes qpdf's @-argument file, one argument per line.
//
// 0600 because it holds a password for as long as qpdf takes to read it. The
// job directory is already 0700 and private to this conversion, so this is
// the second gate rather than the only one.
func writeArgFile(dir string, args []string) (string, error) {
	for _, arg := range args {
		// qpdf reads one argument per line, so an argument containing a
		// newline would become two. No password reaching here can contain
		// one — the API validates that — and this is the check that makes it
		// impossible rather than merely unlikely.
		if strings.ContainsAny(arg, "\r\n") {
			return "", &OptionError{Field: "password", Reason: "A password may not contain a line break."}
		}
	}

	file, err := os.CreateTemp(dir, "qpdf-args-*")
	if err != nil {
		return "", fmt.Errorf("pdf: create an argument file: %w", err)
	}

	name := file.Name()

	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		_ = os.Remove(name)

		return "", fmt.Errorf("pdf: restrict %s: %w", filepath.Base(name), err)
	}

	if _, err := file.WriteString(strings.Join(args, "\n") + "\n"); err != nil {
		_ = file.Close()
		_ = os.Remove(name)

		return "", fmt.Errorf("pdf: write an argument file: %w", err)
	}

	if err := file.Close(); err != nil {
		_ = os.Remove(name)

		return "", fmt.Errorf("pdf: close an argument file: %w", err)
	}

	return name, nil
}

// yesNo renders a boolean as qpdf spells it.
func yesNo(b bool) string {
	if b {
		return "y"
	}

	return "n"
}
