package tests

import (
	"testing"

	"github.com/AlexGalhardo/vukem/i18n"
	"github.com/AlexGalhardo/vukem/vukemtest"

	"github.com/AlexGalhardo/vukem-tools/app/accounts"
	"github.com/AlexGalhardo/vukem-tools/app/mail"
	"github.com/AlexGalhardo/vukem-tools/bootstrap"
)

// The app ships English only (docs/brainstorming.md, 3.6), so every auth
// message must resolve in English, and an unknown locale must fall back to it
// rather than leaking the message key into the UI. The app's own catalog still
// wins, which is how a message gets reworded without editing app/accounts/lang.
func TestAuthMessages(t *testing.T) {
	t.Parallel()

	app := vukemtest.New(t, bootstrap.New)

	acc, err := accounts.New(app.App)
	if err != nil {
		t.Fatal(err)
	}

	if locales := acc.Messages.Locales(); len(locales) != 1 || locales[0] != "en" {
		t.Fatalf("auth message locales = %v, want [en] only", locales)
	}

	keys := acc.Messages.Messages("en")
	if len(keys) == 0 {
		t.Fatal("no auth messages found in the en catalog")
	}

	for key := range keys {
		if got := acc.T(t.Context(), key); got == key || got == "" {
			t.Errorf("auth message %q does not resolve in en (got %q)", key, got)
		}
	}

	const key = "auth.password_reset"

	en := acc.T(t.Context(), key)
	if en == key {
		t.Fatalf("T(%q) returned the key", key)
	}

	// An unknown locale falls back to English instead of returning the key.
	if got := acc.T(i18n.WithLocale(t.Context(), "pt-BR"), key); got != en {
		t.Errorf("T(%q) in an unknown locale = %q, want the English %q", key, got, en)
	}

	own, err := i18n.New(i18n.Options{})
	if err != nil {
		t.Fatal(err)
	}

	if err := own.Add("en", map[string]any{key: "Done."}); err != nil {
		t.Fatal(err)
	}

	if got := acc.T(i18n.WithBundle(t.Context(), own), key); got != "Done." {
		t.Errorf("T(%q) with the app's catalog = %q, want %q", key, got, "Done.")
	}
}

// Mailables built without a translated subject keep an English one.
func TestAuthMailSubjectFallback(t *testing.T) {
	t.Parallel()

	if got := (mail.VerifyEmail{}).Envelope().Subject; got != "Verify your email address" {
		t.Errorf("VerifyEmail subject = %q", got)
	}

	if got := (mail.ResetPassword{}).Envelope().Subject; got != "Reset your password" {
		t.Errorf("ResetPassword subject = %q", got)
	}
}
