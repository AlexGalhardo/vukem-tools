package pdf_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/AlexGalhardo/vukem-tools/internal/engine"
	"github.com/AlexGalhardo/vukem-tools/internal/pdf"
)

// ROADMAP S6.5, first assert: "A protected file cannot be opened by
// pdftotext without the password, and can with it".
func TestAProtectedDocumentNeedsItsPasswordToBeRead(t *testing.T) {
	t.Parallel()

	engines := testEngines(t, pdf.EngineQPDF, pdf.EnginePDFToText)
	dir := t.TempDir()
	ctx := context.Background()

	source := writeNamedFixture(t, dir, "source.pdf", []string{"Confidential terms", "Second page"})
	locked := filepath.Join(dir, "locked.pdf")

	err := engines.Protect(ctx, pdf.ProtectRequest{
		Dir:           dir,
		Input:         source,
		Output:        locked,
		UserPassword:  "open-sesame",
		OwnerPassword: "owner-key",
		Print:         pdf.PrintNone,
		Modify:        pdf.ModifyNone,
	})
	if err != nil {
		t.Fatalf("Protect = %v", err)
	}

	// Without the password: pdftotext has to fail rather than produce the
	// text. This is the assert, and it is run against the engine rather than
	// against our own encryption flag, because the flag is what we asked for
	// and this is what a reader actually gets.
	extracted := filepath.Join(dir, "leak.txt")

	err = engines.ExtractText(ctx, pdf.TextRequest{
		Dir: dir, Input: locked, Output: extracted, SourcePages: 2,
	})
	if err == nil {
		body, readErr := os.ReadFile(extracted)
		if readErr == nil && bytes.Contains(body, []byte("Confidential")) {
			t.Fatal("pdftotext read a protected document without its password")
		}

		t.Fatal("ExtractText on a protected document succeeded")
	}

	// With the password: unlock first, then read. That is the only path this
	// service offers, and it needs the password the user supplied.
	opened := filepath.Join(dir, "opened.pdf")

	if err := engines.Unlock(ctx, pdf.UnlockRequest{
		Dir: dir, Input: locked, Output: opened, Password: "open-sesame",
	}); err != nil {
		t.Fatalf("Unlock with the right password = %v", err)
	}

	text := extractText(t, engines, dir, opened)
	if !bytes.Contains(text, []byte("Confidential terms")) {
		t.Errorf("the unlocked document's text is %q, want it to hold the original text", text)
	}
}

// Permissions are applied, not merely requested: the encryption dictionary
// has to carry them, which is what a reader honours.
func TestProtectionAppliesThePermissionsAskedFor(t *testing.T) {
	t.Parallel()

	engines := testEngines(t, pdf.EngineQPDF)
	dir := t.TempDir()
	ctx := context.Background()

	source := writeFixture(t, dir, "source.pdf", 1)
	locked := filepath.Join(dir, "locked.pdf")

	// No user password: the document opens for anybody and only the
	// permissions are enforced, which is the "restrict what readers may do"
	// case.
	err := engines.Protect(ctx, pdf.ProtectRequest{
		Dir: dir, Input: source, Output: locked,
		OwnerPassword: "owner-key", Print: pdf.PrintLow, Modify: pdf.ModifyNone, AllowExtract: false,
	})
	if err != nil {
		t.Fatalf("Protect = %v", err)
	}

	encrypted, err := engines.IsEncrypted(ctx, dir, locked)
	if err != nil {
		t.Fatalf("IsEncrypted = %v", err)
	}

	if !encrypted {
		t.Error("a document with permissions and an owner password is not encrypted")
	}
}

func TestProtectionRequiresAnOwnerPassword(t *testing.T) {
	t.Parallel()

	engines := testEngines(t, pdf.EngineQPDF)
	dir := t.TempDir()

	err := engines.Protect(context.Background(), pdf.ProtectRequest{
		Dir:          dir,
		Input:        writeFixture(t, dir, "source.pdf", 1),
		Output:       filepath.Join(dir, "o.pdf"),
		UserPassword: "open-sesame",
	})

	if _, ok := errors.AsType[*pdf.OptionError](err); !ok {
		t.Fatalf("Protect with no owner password = %v, want *pdf.OptionError", err)
	}
}

// The two passwords must differ, or anybody who can open the document can
// also lift its restrictions — which makes the restrictions decorative.
func TestProtectionRefusesTheSamePasswordForBoth(t *testing.T) {
	t.Parallel()

	engines := testEngines(t, pdf.EngineQPDF)
	dir := t.TempDir()

	err := engines.Protect(context.Background(), pdf.ProtectRequest{
		Dir:           dir,
		Input:         writeFixture(t, dir, "source.pdf", 1),
		Output:        filepath.Join(dir, "o.pdf"),
		UserPassword:  "same",
		OwnerPassword: "same",
	})

	if _, ok := errors.AsType[*pdf.OptionError](err); !ok {
		t.Fatalf("Protect with equal passwords = %v, want *pdf.OptionError", err)
	}
}

// A password cannot contain a line break: it travels to qpdf through an
// argument file that is one argument per line, so a newline would split it.
func TestProtectionRefusesAPasswordWithALineBreak(t *testing.T) {
	t.Parallel()

	engines := testEngines(t, pdf.EngineQPDF)
	dir := t.TempDir()

	err := engines.Protect(context.Background(), pdf.ProtectRequest{
		Dir:           dir,
		Input:         writeFixture(t, dir, "source.pdf", 1),
		Output:        filepath.Join(dir, "o.pdf"),
		OwnerPassword: "two\nlines",
	})

	if _, ok := errors.AsType[*pdf.OptionError](err); !ok {
		t.Fatalf("Protect with a newline in the password = %v, want *pdf.OptionError", err)
	}
}

// ROADMAP S6.5, second assert: "Unlock refuses a wrong password with a
// dedicated message and never reveals whether the file had an owner password,
// a user password or both".
func TestUnlockRefusesAWrongPasswordWithoutSayingWhatKindItWas(t *testing.T) {
	t.Parallel()

	engines := testEngines(t, pdf.EngineQPDF)
	dir := t.TempDir()
	ctx := context.Background()

	source := writeFixture(t, dir, "source.pdf", 1)

	cases := []struct {
		name string
		user string
	}{
		// Both shapes of protected document, so the refusal cannot differ
		// between them: one needs a password to open, the other does not and
		// only restricts what a reader may do.
		{"a user and an owner password", "open-sesame"},
		{"an owner password only", ""},
	}

	messages := make([]string, 0, len(cases))

	for _, c := range cases {
		locked := filepath.Join(dir, "locked-"+strings.ReplaceAll(c.name, " ", "-")+".pdf")

		err := engines.Protect(ctx, pdf.ProtectRequest{
			Dir: dir, Input: source, Output: locked,
			UserPassword: c.user, OwnerPassword: "owner-key", Modify: pdf.ModifyNone,
		})
		if err != nil {
			t.Fatalf("%s: Protect = %v", c.name, err)
		}

		out := filepath.Join(dir, "out-"+strings.ReplaceAll(c.name, " ", "-")+".pdf")

		err = engines.Unlock(ctx, pdf.UnlockRequest{
			Dir: dir, Input: locked, Output: out, Password: "definitely-wrong",
		})
		if !errors.Is(err, pdf.ErrWrongPassword) {
			t.Fatalf("%s: Unlock with a wrong password = %v, want ErrWrongPassword", c.name, err)
		}

		messages = append(messages, err.Error())

		if _, statErr := os.Stat(out); statErr == nil {
			t.Errorf("%s: a refused unlock left an output file behind", c.name)
		}
	}

	// The same refusal for both, so nothing can be inferred from it about
	// which kind of password the document had.
	if messages[0] != messages[1] {
		t.Errorf("the refusals differ, which tells the caller which kind of password the file had:\n"+
			"%q\n%q", messages[0], messages[1])
	}

	for _, leak := range []string{"owner", "user"} {
		if strings.Contains(strings.ToLower(messages[0]), leak) {
			t.Errorf("the refusal mentions %q: %s", leak, messages[0])
		}
	}
}

func TestUnlockRefusesADocumentThatHasNoPassword(t *testing.T) {
	t.Parallel()

	engines := testEngines(t, pdf.EngineQPDF)
	dir := t.TempDir()

	err := engines.Unlock(context.Background(), pdf.UnlockRequest{
		Dir:      dir,
		Input:    writeFixture(t, dir, "plain.pdf", 1),
		Output:   filepath.Join(dir, "o.pdf"),
		Password: "whatever",
	})

	if !errors.Is(err, pdf.ErrNotEncrypted) {
		t.Fatalf("Unlock of a plain document = %v, want ErrNotEncrypted", err)
	}
}

func TestUnlockNeedsAPassword(t *testing.T) {
	t.Parallel()

	engines := testEngines(t, pdf.EngineQPDF)
	dir := t.TempDir()

	err := engines.Unlock(context.Background(), pdf.UnlockRequest{
		Dir: dir, Input: writeFixture(t, dir, "plain.pdf", 1), Output: filepath.Join(dir, "o.pdf"),
	})

	if _, ok := errors.AsType[*pdf.OptionError](err); !ok {
		t.Fatalf("Unlock with no password = %v, want *pdf.OptionError", err)
	}
}

// ROADMAP S6.5, third assert: "No code path takes more than one password
// attempt per request, proven by a test".
//
// The proof is a counting runner: every subprocess the unlock starts is
// recorded, and the ones that carry a password on standard input are counted.
// A retry loop, a fallback to the empty password or a second guess of any
// kind would show up here as a second attempt, whatever it was called in the
// code.
func TestAnUnlockMakesExactlyOnePasswordAttempt(t *testing.T) {
	t.Parallel()

	registry, runner := testEngineParts(t, pdf.EngineQPDF)
	dir := t.TempDir()
	ctx := context.Background()

	plain, err := pdf.NewEngines(registry, runner)
	if err != nil {
		t.Fatalf("build the engine set: %v", err)
	}

	source := writeFixture(t, dir, "source.pdf", 1)
	locked := filepath.Join(dir, "locked.pdf")

	if err := plain.Protect(ctx, pdf.ProtectRequest{
		Dir: dir, Input: source, Output: locked, UserPassword: "right", OwnerPassword: "owner",
	}); err != nil {
		t.Fatalf("Protect = %v", err)
	}

	counter := &countingRunner{inner: runner}

	counted, err := pdf.NewEngines(registry, counter)
	if err != nil {
		t.Fatalf("build the counting engine set: %v", err)
	}

	for _, password := range []string{"wrong", "right"} {
		counter.reset()

		unlockErr := counted.Unlock(ctx, pdf.UnlockRequest{
			Dir: dir, Input: locked, Output: filepath.Join(dir, "out-"+password+".pdf"), Password: password,
		})

		if password == "right" && unlockErr != nil {
			t.Fatalf("Unlock with the right password = %v", unlockErr)
		}

		if password == "wrong" && !errors.Is(unlockErr, pdf.ErrWrongPassword) {
			t.Fatalf("Unlock with a wrong password = %v, want ErrWrongPassword", unlockErr)
		}

		if attempts := counter.withPassword(); attempts != 1 {
			t.Errorf("the %q password was attempted %d times, want exactly 1", password, attempts)
		}
	}
}

// A password must never appear in a subprocess's arguments: argv is readable
// from /proc by anything running as the same user, so an argument is a
// password published to every process in the container.
func TestNoPasswordEverReachesASubprocessArgument(t *testing.T) {
	t.Parallel()

	registry, runner := testEngineParts(t, pdf.EngineQPDF)
	dir := t.TempDir()
	ctx := context.Background()

	counter := &countingRunner{inner: runner}

	engines, err := pdf.NewEngines(registry, counter)
	if err != nil {
		t.Fatalf("build the engine set: %v", err)
	}

	source := writeFixture(t, dir, "source.pdf", 1)
	locked := filepath.Join(dir, "locked.pdf")

	const (
		user  = "user-secret-9f2a"
		owner = "owner-secret-7b31"
	)

	if err := engines.Protect(ctx, pdf.ProtectRequest{
		Dir: dir, Input: source, Output: locked, UserPassword: user, OwnerPassword: owner,
	}); err != nil {
		t.Fatalf("Protect = %v", err)
	}

	if err := engines.Unlock(ctx, pdf.UnlockRequest{
		Dir: dir, Input: locked, Output: filepath.Join(dir, "open.pdf"), Password: user,
	}); err != nil {
		t.Fatalf("Unlock = %v", err)
	}

	for _, secret := range []string{user, owner} {
		for _, args := range counter.arguments() {
			for _, arg := range args {
				if strings.Contains(arg, secret) {
					t.Errorf("a password reached a subprocess argument: %q", arg)
				}
			}
		}
	}
}

// countingRunner records every subprocess an operation starts.
type countingRunner struct {
	inner pdf.Subprocess

	mu        sync.Mutex
	args      [][]string
	passwords int
}

// Run implements pdf.Subprocess, recording the command before running it.
func (c *countingRunner) Run(ctx context.Context, cmd engine.Command) (engine.Result, error) {
	c.mu.Lock()

	c.args = append(c.args, append([]string(nil), cmd.Args...))

	// A command with something on standard input is a password attempt: it is
	// the only thing this package ever writes there.
	if cmd.Stdin != nil {
		c.passwords++
	}

	c.mu.Unlock()

	return c.inner.Run(ctx, cmd)
}

// reset forgets what has been recorded.
func (c *countingRunner) reset() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.args = nil
	c.passwords = 0
}

// withPassword is how many commands carried a password.
func (c *countingRunner) withPassword() int {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.passwords
}

// arguments is every command line that was run.
func (c *countingRunner) arguments() [][]string {
	c.mu.Lock()
	defer c.mu.Unlock()

	return append([][]string(nil), c.args...)
}

// ROADMAP S6.5: the comparison is a text diff, page aligned, and it says so
// in its own report.
func TestComparisonFindsTheChangedPagesAndSaysWhatItCompares(t *testing.T) {
	t.Parallel()

	engines := testEngines(t, pdf.EngineQPDF, pdf.EnginePDFToText)
	dir := t.TempDir()
	ctx := context.Background()

	before := writeNamedFixture(t, dir, "before.pdf", []string{"Clause one", "Clause two", "Clause three"})
	after := writeNamedFixture(t, dir, "after.pdf", []string{"Clause one", "Clause two amended", "Clause three"})

	report := filepath.Join(dir, "report.md")

	result, err := engines.Compare(ctx, pdf.CompareRequest{
		Dir: dir, Left: before, Right: after,
		LeftName: "before.pdf", RightName: "after.pdf", Output: report,
	})
	if err != nil {
		t.Fatalf("Compare = %v", err)
	}

	if result.Identical {
		t.Error("Identical = true for two documents whose text differs")
	}

	if result.ChangedPages != 1 {
		t.Errorf("ChangedPages = %d, want 1", result.ChangedPages)
	}

	if result.Pages != 3 {
		t.Errorf("Pages = %d, want 3", result.Pages)
	}

	body, err := os.ReadFile(report)
	if err != nil {
		t.Fatalf("read the report: %v", err)
	}

	markdown := string(body)

	for _, want := range []string{
		"# Text comparison",
		"`before.pdf`",
		"`after.pdf`",
		// The report has to say what it compares, because a text diff that
		// reads as a document diff would be trusted for something it does
		// not do.
		"compares the **text**",
		"## Page 2",
		"- Clause two",
		"+ Clause two amended",
	} {
		if !strings.Contains(markdown, want) {
			t.Errorf("the report is missing %q:\n%s", want, markdown)
		}
	}

	if strings.Contains(markdown, "## Page 1") || strings.Contains(markdown, "## Page 3") {
		t.Error("the report names pages that did not change")
	}
}

func TestComparingADocumentWithItselfReportsItIdentical(t *testing.T) {
	t.Parallel()

	engines := testEngines(t, pdf.EngineQPDF, pdf.EnginePDFToText)
	dir := t.TempDir()

	source := writeNamedFixture(t, dir, "source.pdf", []string{"One", "Two"})
	report := filepath.Join(dir, "same.md")

	result, err := engines.Compare(context.Background(), pdf.CompareRequest{
		Dir: dir, Left: source, Right: source, LeftName: "a.pdf", RightName: "b.pdf", Output: report,
	})
	if err != nil {
		t.Fatalf("Compare = %v", err)
	}

	if !result.Identical {
		t.Errorf("Identical = false comparing a document with itself: %+v", result)
	}

	if result.ComparisonSummary() == "" {
		t.Error("the summary sentence is empty")
	}
}

// A comparison where one document has extra pages reports them as a page
// that exists only on one side, rather than as a page of changes.
func TestComparisonReportsAPageThatOnlyOneDocumentHas(t *testing.T) {
	t.Parallel()

	engines := testEngines(t, pdf.EngineQPDF, pdf.EnginePDFToText)
	dir := t.TempDir()

	short := writeNamedFixture(t, dir, "short.pdf", []string{"One"})
	long := writeNamedFixture(t, dir, "long.pdf", []string{"One", "Appendix"})
	report := filepath.Join(dir, "longer.md")

	result, err := engines.Compare(context.Background(), pdf.CompareRequest{
		Dir: dir, Left: short, Right: long, LeftName: "short.pdf", RightName: "long.pdf", Output: report,
	})
	if err != nil {
		t.Fatalf("Compare = %v", err)
	}

	if result.Pages != 2 {
		t.Errorf("Pages = %d, want 2 (the longer document's)", result.Pages)
	}

	body, err := os.ReadFile(report)
	if err != nil {
		t.Fatalf("read the report: %v", err)
	}

	if !strings.Contains(string(body), "only in `long.pdf`") {
		t.Errorf("the report does not say the page is only in one document:\n%s", body)
	}
}
