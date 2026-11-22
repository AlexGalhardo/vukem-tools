package tests

import (
	"net/http"
	"os"
	"path/filepath"
	"regexp"
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

var resetPath = regexp.MustCompile(`^/reset-password/([A-Za-z0-9]+)\?email=`)

// Local reset: the one-time code is in the response, no mail is queued, the
// code works once, and the old password stops working.
func TestLocalPasswordResetShowsTheCodeInsteadOfMailingIt(t *testing.T) {
	t.Parallel()

	app, fake := localAccountsApp(t)

	app.Client(t).Inertia("").PostJSON("/register", signUpForm("Ana", "ana@example.com", testPassword)).
		AssertRedirect("/dashboard")

	anon := app.Client(t).Inertia("").WithHeader("Referer", "/login")
	res := anon.PostJSON("/forgot-password", map[string]any{"email": "ana@example.com"})
	res.AssertStatus(http.StatusSeeOther)

	loc := res.Recorder().Header().Get("Location")

	m := resetPath.FindStringSubmatch(loc)
	if m == nil {
		t.Fatalf("the reset answer does not carry a code: Location %q", loc)
	}

	if n := mailJobCount(fake); n != 0 {
		t.Fatalf("local reset queued %d mails, want none", n)
	}

	reset := map[string]any{
		"email": "ana@example.com", "token": m[1],
		"password": newPassword, "password_confirmation": newPassword,
	}
	anon.PostJSON("/reset-password", reset).AssertRedirect("/login")
	app.Client(t).
		Inertia("").
		WithHeader("Referer", "/reset-password").
		PostJSON("/reset-password", reset).
		AssertRedirect("/reset-password")
		// single use

	anon.PostJSON("/login", map[string]any{"email": "ana@example.com", "password": testPassword}).
		AssertRedirect("/login")
	anon.PostJSON("/login", map[string]any{"email": "ana@example.com", "password": newPassword}).
		AssertRedirect("/dashboard")
}

// An unknown address gets no code, so the code cannot be minted for an
// account that does not exist.
func TestLocalPasswordResetForAnUnknownEmailGivesNoCode(t *testing.T) {
	t.Parallel()

	app, _ := localAccountsApp(t)

	res := app.Client(t).Inertia("").PostJSON("/forgot-password", map[string]any{"email": "nobody@example.com"})
	if loc := res.Recorder().Header().Get("Location"); resetPath.MatchString(loc) {
		t.Fatalf("a code was issued for an unknown email: %q", loc)
	}

	vukemtest.AssertDatabaseCount(t, app, "password_resets", 0)
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
