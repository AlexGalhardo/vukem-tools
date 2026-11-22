package tests

import (
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"

	vukem "github.com/AlexGalhardo/vukem"
	"github.com/AlexGalhardo/vukem/queue"
	"github.com/AlexGalhardo/vukem/vukemtest"

	"github.com/AlexGalhardo/vukem-tools/bootstrap"
)

// localAccountsApp boots local mode with a fake queue, so a test can count the
// mails that would have gone out. APP_URL is the host vukemtest's client
// sends (example.com): the Host guard accepts the configured address next to
// loopback, so the suite's relative paths still reach the app.
func localAccountsApp(t *testing.T) (*vukemtest.App, *queue.Fake) {
	t.Helper()

	dir := filepath.Join(t.TempDir(), "data")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { removeWithRetry(t, dir) })

	env := bootstrap.LocalEnv(dir)
	env["APP_URL"] = "http://example.com"

	fake := queue.NewFake()

	return vukemtest.New(t, bootstrap.New, vukemtest.Options{
		Env:        env,
		AppOptions: []vukem.Option{vukem.WithQueueDriver(fake)},
	}), fake
}

func hostedAccountsApp(t *testing.T) (*vukemtest.App, *queue.Fake) {
	t.Helper()

	fake := queue.NewFake()

	return vukemtest.New(t, bootstrap.New, vukemtest.Options{
		AppOptions: []vukem.Option{vukem.WithQueueDriver(fake)},
	}), fake
}

func mailJobCount(fake *queue.Fake) int {
	n := 0

	msgs := fake.Messages()
	for i := range msgs {
		if msgs[i].Kind == vukem.MailJobKind {
			n++
		}
	}

	return n
}

func requestKey(c *vukemtest.Client, email, password string) *vukemtest.Response {
	return c.PostJSON("/api/v1/tokens", map[string]any{
		"email": email, "password": password, "device_name": "cli", "abilities": []string{"image:convert"},
	})
}

// Acceptance 1: no network, no mailbox. A new local account signs up, is
// signed in, and can mint a key, with no mail queued.
func TestLocalFirstAccountSignsUpVerifiedAndGetsAKey(t *testing.T) {
	t.Parallel()

	app, fake := localAccountsApp(t)
	web := app.Client(t).Inertia("").WithHeader("Referer", "/register")

	web.PostJSON("/register", signUpForm("Ana", "ana@example.com", testPassword)).AssertRedirect("/dashboard")
	web.Get("/dashboard").AssertInertia("Dashboard").PropEquals("auth.user.email", "ana@example.com")

	db, err := app.DB()
	if err != nil {
		t.Fatal(err)
	}

	var verified int
	if err := db.QueryRowContext(t.Context(),
		"SELECT COUNT(*) FROM users WHERE email = 'ana@example.com' AND email_verified_at IS NOT NULL").
		Scan(&verified); err != nil || verified != 1 {
		t.Fatalf("the first local account is not verified (count %d, err %v)", verified, err)
	}

	requestKey(app.Client(t), "ana@example.com", testPassword).AssertCreated()

	web.PostJSON("/account/keys", map[string]any{"name": "mcp", "abilities": []string{"image:convert"}}).
		AssertStatus(http.StatusSeeOther)

	if n := mailJobCount(fake); n != 0 {
		t.Fatalf("local sign-up queued %d mails, want none", n)
	}

	// Sign in again with nothing but the password.
	web.PostJSON("/logout", nil).AssertRedirect("/")
	app.Client(t).Inertia("").PostJSON("/login", map[string]any{"email": "ana@example.com", "password": testPassword}).
		AssertRedirect("/dashboard")
}

// Only the first account is trusted: the machine's owner. A second one follows
// the hosted rule, because "at the keyboard" says nothing about who it is for.
func TestLocalSecondAccountStaysUnverifiedAndGetsNoKey(t *testing.T) {
	t.Parallel()

	app, fake := localAccountsApp(t)

	app.Client(t).Inertia("").PostJSON("/register", signUpForm("Ana", "ana@example.com", testPassword)).
		AssertRedirect("/dashboard")

	bea := app.Client(t).Inertia("").WithHeader("Referer", "/login")
	bea.PostJSON("/register", signUpForm("Bea", "bea@example.com", otherPassword)).AssertRedirect("/dashboard")

	vukemtest.AssertDatabaseHas(t, app, "users", map[string]any{"email": "bea@example.com", "email_verified_at": nil})
	requestKey(app.Client(t), "bea@example.com", otherPassword).AssertStatus(http.StatusForbidden)

	device, user := startLink(t, app)
	bea.PostJSON("/account/link", map[string]any{"code": user, "decision": "approve"}).AssertRedirect("/login")
	pollLink(t, app, device).AssertJSONPath("status", "pending")

	if n := mailJobCount(fake); n != 0 {
		t.Fatalf("local sign-up queued %d mails, want none", n)
	}
}

func TestLocalSignUpWithATakenEmailIsRefusedWithoutMail(t *testing.T) {
	t.Parallel()

	app, fake := localAccountsApp(t)

	app.Client(t).Inertia("").PostJSON("/register", signUpForm("Ana", "ana@example.com", testPassword)).
		AssertRedirect("/dashboard")

	app.Client(t).Inertia("").WithHeader("Referer", "/register").
		PostJSON("/register", signUpForm("Mallory", "ANA@example.com", otherPassword)).
		AssertRedirect("/register")

	vukemtest.AssertDatabaseCount(t, app, "users", 1)

	if n := mailJobCount(fake); n != 0 {
		t.Fatalf("queued %d mails, want none", n)
	}
}

var codeLine = regexp.MustCompile(`(?m)^code: ([A-Za-z0-9]+)$`)

func resetFile(app *vukemtest.App) string {
	return filepath.Join(app.Source.Get("DATA_DIR"), "password-reset.txt")
}

// readResetCode returns the code the local reset wrote to the data directory.
func readResetCode(t *testing.T, app *vukemtest.App) string {
	t.Helper()

	raw, err := os.ReadFile(resetFile(app))
	if err != nil {
		t.Fatalf("the reset code file was not written: %v", err)
	}

	m := codeLine.FindSubmatch(raw)
	if m == nil {
		t.Fatalf("no code in the reset file:\n%s", raw)
	}

	return string(m[1])
}

func forgot(t *testing.T, app *vukemtest.App, email string) *vukemtest.Response {
	t.Helper()

	return app.Client(t).Inertia("").WithHeader("Referer", "/forgot-password").
		PostJSON("/forgot-password", map[string]any{"email": email})
}

func submitReset(t *testing.T, app *vukemtest.App, code string) *vukemtest.Response {
	t.Helper()

	return app.Client(t).Inertia("").WithHeader("Referer", "/reset-password").
		PostJSON("/reset-password", map[string]any{
			"email": "ana@example.com", "token": code, "password": newPassword, "password_confirmation": newPassword,
		})
}

// Local reset: no secret ever goes to an unauthenticated HTTP caller. The code
// is written to a file in the data directory, so reading it proves access to
// that directory; the answer is the same for a known and an unknown email.
func TestLocalPasswordResetWritesTheCodeToAFileAndNeverAnswersWithIt(t *testing.T) {
	t.Parallel()

	app, fake := localAccountsApp(t)

	app.Client(t).Inertia("").PostJSON("/register", signUpForm("Ana", "ana@example.com", testPassword)).
		AssertRedirect("/dashboard")

	unknown := forgot(t, app, "nobody@example.com")
	unknown.AssertRedirect("/reset-password")

	if _, err := os.Stat(resetFile(app)); err == nil {
		t.Fatal("a code file was written for an unknown email")
	}

	vukemtest.AssertDatabaseCount(t, app, "password_resets", 0)

	known := forgot(t, app, "ana@example.com")
	known.AssertRedirect("/reset-password")

	code := readResetCode(t, app)
	if len(code) < 20 {
		t.Fatalf("the code %q is too short", code)
	}

	for name, res := range map[string]*vukemtest.Response{"known": known, "unknown": unknown} {
		rec := res.Recorder()

		if strings.Contains(rec.Body.String(), code) {
			t.Errorf("%s: the body carries the code", name)
		}

		for k, vs := range rec.Header() {
			for _, v := range vs {
				if strings.Contains(v, code) {
					t.Errorf("%s: header %s carries the code", name, k)
				}
			}
		}
	}

	if a, b := known.Recorder(), unknown.Recorder(); a.Code != b.Code ||
		a.Header().Get("Location") != b.Header().Get("Location") || a.Body.String() != b.Body.String() {
		t.Errorf("the answers differ: known %d %q %q, unknown %d %q %q",
			a.Code, a.Header().Get("Location"), a.Body.String(), b.Code, b.Header().Get("Location"), b.Body.String())
	}

	if n := mailJobCount(fake); n != 0 {
		t.Fatalf("local reset queued %d mails, want none", n)
	}

	t.Run("file mode", func(t *testing.T) {
		t.Parallel()

		if runtime.GOOS == "windows" {
			t.Skip("Windows has no POSIX permission bits; the 0600 mode is asserted on Linux and macOS")
		}

		info, err := os.Stat(resetFile(app))
		if err != nil {
			t.Fatal(err)
		}

		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Fatalf("the code file has mode %o, want 600", perm)
		}
	})

	raw, _ := os.ReadFile(resetFile(app))
	if !strings.Contains(string(raw), "email: ana@example.com") || !strings.Contains(string(raw), "expires: ") {
		t.Errorf("the file lacks the email or the expiry:\n%s", raw)
	}
}

// The reset page is reached without the code in the URL and says where the
// file is; the right code resets the password once.
func TestLocalPasswordResetWithTheCodeFromTheFileWorksOnce(t *testing.T) {
	t.Parallel()

	app, _ := localAccountsApp(t)

	app.Client(t).Inertia("").PostJSON("/register", signUpForm("Ana", "ana@example.com", testPassword)).
		AssertRedirect("/dashboard")

	forgot(t, app, "ana@example.com").AssertRedirect("/reset-password")

	app.Client(t).Inertia("").Get("/reset-password").AssertInertia("Auth/ResetPassword").
		PropEquals("token", "").PropEquals("codeFile", resetFile(app))

	code := readResetCode(t, app)

	submitReset(t, app, code).AssertRedirect("/login")
	submitReset(t, app, code).AssertRedirect("/reset-password") // single use

	if _, err := os.Stat(resetFile(app)); err == nil {
		t.Error("the spent code is still in the file")
	}

	login := app.Client(t).Inertia("").WithHeader("Referer", "/login")
	login.PostJSON("/login", map[string]any{"email": "ana@example.com", "password": testPassword}).
		AssertRedirect("/login")
	login.PostJSON("/login", map[string]any{"email": "ana@example.com", "password": newPassword}).
		AssertRedirect("/dashboard")
}

// A wrong code is refused, and five wrong tries for an email invalidate the
// code: the right one is refused afterwards and the stored reset is gone.
func TestLocalPasswordResetIsInvalidatedAfterFiveWrongCodes(t *testing.T) {
	t.Parallel()

	app, _ := localAccountsApp(t)

	app.Client(t).Inertia("").PostJSON("/register", signUpForm("Ana", "ana@example.com", testPassword)).
		AssertRedirect("/dashboard")

	forgot(t, app, "ana@example.com").AssertRedirect("/reset-password")

	code := readResetCode(t, app)

	for i := range 5 {
		submitReset(t, app, "WRONGCODE"+strconv.Itoa(i)).AssertRedirect("/reset-password")
	}

	vukemtest.AssertDatabaseCount(t, app, "password_resets", 0)
	submitReset(t, app, code).AssertRedirect("/reset-password")

	login := app.Client(t).Inertia("").WithHeader("Referer", "/login")
	login.PostJSON("/login", map[string]any{"email": "ana@example.com", "password": testPassword}).
		AssertRedirect("/dashboard")
}

// Four wrong tries do not spoil the code.
func TestLocalPasswordResetSurvivesFourWrongCodes(t *testing.T) {
	t.Parallel()

	app, _ := localAccountsApp(t)

	app.Client(t).Inertia("").PostJSON("/register", signUpForm("Ana", "ana@example.com", testPassword)).
		AssertRedirect("/dashboard")

	forgot(t, app, "ana@example.com").AssertRedirect("/reset-password")

	code := readResetCode(t, app)

	for i := range 4 {
		submitReset(t, app, "WRONGCODE"+strconv.Itoa(i)).AssertRedirect("/reset-password")
	}

	submitReset(t, app, code).AssertRedirect("/login")
}

// Acceptance 2, the regression that matters most: nothing above leaks into
// hosted mode. A first hosted account is unverified, gets no key and cannot
// approve a device link, and the reset answer carries no code.
func TestHostedFirstAccountStaysUnverifiedAndGetsNoKey(t *testing.T) {
	t.Parallel()

	app, fake := hostedAccountsApp(t)

	web := app.Client(t).Inertia("").WithHeader("Referer", "/login")
	web.PostJSON("/register", signUpForm("Ana", "ana@example.com", testPassword)).
		AssertRedirect("/register/check-email")

	vukemtest.AssertDatabaseHas(t, app, "users", map[string]any{"email": "ana@example.com", "email_verified_at": nil})
	requestKey(app.Client(t), "ana@example.com", testPassword).AssertStatus(http.StatusForbidden)

	web.PostJSON("/login", map[string]any{"email": "ana@example.com", "password": testPassword}).
		AssertRedirect("/dashboard")

	device, user := startLink(t, app)
	web.PostJSON("/account/link", map[string]any{"code": user, "decision": "approve"}).AssertRedirect("/login")
	pollLink(t, app, device).AssertJSONPath("status", "pending")
	vukemtest.AssertDatabaseCount(t, app, "access_tokens", 0)

	res := app.Client(t).Inertia("").PostJSON("/forgot-password", map[string]any{"email": "ana@example.com"})
	res.AssertRedirect("/forgot-password")

	if n := mailJobCount(fake); n != 1 {
		t.Fatalf("hosted reset queued %d mails, want 1", n)
	}
}

// Acceptance 3: a Host that is not loopback is refused in local mode, so a
// page on another origin that rebinds its name to 127.0.0.1 reaches nothing.
func TestLocalModeRefusesAForeignHost(t *testing.T) {
	t.Parallel()

	app, _ := localAccountsApp(t)
	c := app.Client(t)

	for _, host := range []string{"evil.test", "evil.test:3000", "127.0.0.1.evil.test", "192.168.1.20:3000"} {
		for _, path := range []string{"/", "/login", "/health", "/api/v1/me"} {
			c.Get("http://" + host + path).AssertStatus(http.StatusForbidden)
		}
	}

	for _, host := range []string{"localhost:3000", "127.0.0.1:3000", "[::1]:3000", "LOCALHOST"} {
		c.Get("http://" + host + "/login").AssertOK()
	}
}

func TestHostedModeDoesNotGuardTheHost(t *testing.T) {
	t.Parallel()

	app, _ := hostedAccountsApp(t)

	app.Client(t).Get("http://tools.example.org/login").AssertOK()
}
