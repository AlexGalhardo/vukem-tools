package accounts

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/AlexGalhardo/vukem/auth"
	"github.com/AlexGalhardo/vukem/database"
	"github.com/AlexGalhardo/vukem/errs"

	"github.com/AlexGalhardo/vukem-tools/app/mail"
	"github.com/AlexGalhardo/vukem-tools/app/models"
)

// SendResetLink emails a single-use password reset link. Unknown emails
// are silently ignored so the form never reveals who has an account; the
// mail is queued (SendLater) so the answer does not wait for the mail
// server either, whose latency would tell known emails apart. Use
// QUEUE_CONNECTION=database (memory loses queued mail on restart; sync
// sends it inline).
func (a *Accounts) SendResetLink(ctx context.Context, email string) error {
	email = strings.ToLower(email)

	u, err := a.Users.ByCredentials(ctx, map[string]string{"email": email})
	if errors.Is(err, auth.ErrUserNotFound) {
		return nil
	}

	if err != nil {
		return err
	}

	token, err := a.storeResetToken(ctx, email)
	if err != nil {
		return err
	}

	link := a.URL + "/reset-password/" + token + "?email=" + url.QueryEscape(email)

	return a.Mailer.SendLater(
		ctx,
		mail.ResetPassword{User: u, Link: link, Subject: a.T(ctx, "auth.mail.reset_subject")},
	)
}

// resetFileName is the file in the data directory that holds the pending
// local reset code.
const resetFileName = "password-reset.txt"

// maxResetAttempts is how many wrong codes an email may try before its code is
// invalidated.
const maxResetAttempts = 5

// LocalResetFile is where a local reset writes its code ("" when the data
// directory is unknown).
func (a *Accounts) LocalResetFile() string {
	if a.dataDir == "" {
		return ""
	}

	return filepath.Join(a.dataDir, resetFileName)
}

// LocalRequestReset starts a password reset on a machine with no mailbox. For
// an account it creates the single-use code (ResetTTL) and writes it, with the
// email and the expiry, to LocalResetFile, readable by its owner only: whoever
// can read the data directory can already read the database, so reading the
// file proves what the code is for. For an unknown email it does nothing. It
// returns nothing about which of the two happened, and the HTTP answer must not
// either: no secret goes to an unauthenticated caller. Local mode only.
func (a *Accounts) LocalRequestReset(ctx context.Context, email string) error {
	if !a.Local || a.dataDir == "" {
		return errors.New("accounts: local reset needs local mode and a data directory")
	}

	email = strings.ToLower(strings.TrimSpace(email))

	if _, err := a.Users.ByCredentials(ctx, map[string]string{"email": email}); err != nil {
		if errors.Is(err, auth.ErrUserNotFound) {
			return nil
		}

		return err
	}

	code, err := a.storeResetToken(ctx, email)
	if err != nil {
		return err
	}

	// A fresh code starts a fresh count.
	c, err := a.cache()
	if err != nil {
		return err
	}

	if err := c.Forget(ctx, resetAttemptsKey(email)); err != nil {
		return err
	}

	body := fmt.Sprintf(
		"email: %s\ncode: %s\nexpires: %s\n",
		email,
		code,
		a.now().UTC().Add(a.ResetTTL).Format(time.RFC3339),
	)

	return writeOwnerOnly(a.LocalResetFile(), body)
}

// writeOwnerOnly writes body to path with mode 0600, narrowing a file that
// already exists with a wider one.
func writeOwnerOnly(path, body string) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("accounts: write the reset code: %w", err)
	}

	if err := f.Chmod(0o600); err != nil && runtime.GOOS != "windows" {
		_ = f.Close()

		return fmt.Errorf("accounts: restrict the reset code file: %w", err)
	}

	if _, err := f.WriteString(body); err != nil {
		_ = f.Close()

		return fmt.Errorf("accounts: write the reset code: %w", err)
	}

	return f.Close()
}

func resetAttemptsKey(email string) string { return "reset:attempts:" + EmailHash(email) }

// noteWrongResetCode counts a wrong code for email and, at maxResetAttempts,
// deletes the pending reset so even the right code stops working.
func (a *Accounts) noteWrongResetCode(ctx context.Context, email string) error {
	c, err := a.cache()
	if err != nil {
		return err
	}

	n, err := c.Increment(ctx, resetAttemptsKey(email), 1, a.ResetTTL)
	if err != nil {
		return err
	}

	if n < maxResetAttempts {
		return nil
	}

	d := a.DB.Dialect()
	_, err = a.DB.ExecContext(ctx, "DELETE FROM password_resets WHERE email = "+d.Placeholder(1), email)

	return err
}

// storeResetToken replaces the pending reset of email with a new random token
// and returns it; only its digest is stored.
func (a *Accounts) storeResetToken(ctx context.Context, email string) (string, error) {
	token := rand.Text()
	d := a.DB.Dialect()

	if _, err := a.DB.ExecContext(
		ctx,
		"DELETE FROM password_resets WHERE email = "+d.Placeholder(1),
		email,
	); err != nil {
		return "", err
	}

	if _, err := a.DB.ExecContext(ctx, "INSERT INTO password_resets (email, hash, created_at) VALUES ("+
		d.Placeholder(1)+", "+d.Placeholder(2)+", "+d.Placeholder(3)+")",
		email, digest(token), a.now().UTC()); err != nil {
		return "", err
	}

	return token, nil
}

func (a *Accounts) errInvalidReset(ctx context.Context) error {
	return errs.NewValidation().Add("email", a.T(ctx, "auth.reset_invalid"))
}

// ResetPassword checks the token (constant time, single use, ResetTTL) and
// sets the new password, ending every session, remembered session and API
// token of the user.
func (a *Accounts) ResetPassword(ctx context.Context, email, token, password string) (models.User, error) {
	email = strings.ToLower(email)
	d := a.DB.Dialect()

	var (
		stored  string
		created time.Time
	)

	err := a.DB.QueryRowContext(ctx, "SELECT hash, created_at FROM password_resets WHERE email = "+d.Placeholder(1), email).
		Scan(&stored, &created)
	if err != nil {
		return models.User{}, a.errInvalidReset(ctx)
	}

	if subtle.ConstantTimeCompare([]byte(stored), []byte(digest(token))) != 1 {
		if a.Local {
			if err := a.noteWrongResetCode(ctx, email); err != nil {
				return models.User{}, err
			}
		}

		return models.User{}, a.errInvalidReset(ctx)
	}

	if a.expired(created, a.ResetTTL) {
		return models.User{}, a.errInvalidReset(ctx)
	}

	u, err := a.Users.ByCredentials(ctx, map[string]string{"email": email})
	if err != nil {
		return models.User{}, a.errInvalidReset(ctx)
	}

	err = a.DB.Tx(ctx, func(tx *database.Tx) error {
		res, err := tx.ExecContext(ctx, "DELETE FROM password_resets WHERE email = "+d.Placeholder(1), email)
		if err != nil {
			return err
		}

		// A concurrent reset with the same token already used it.
		if n, err := res.RowsAffected(); err != nil || n != 1 {
			return a.errInvalidReset(ctx)
		}

		// A new password ends every other way in, together with the
		// password change: remembered sessions, API tokens issued with the
		// old one and, through the session version, every browser session
		// on every session driver.
		if err := a.revokeAccess(ctx, tx, u.ID); err != nil {
			return err
		}

		return a.updatePassword(ctx, tx, u.ID, password)
	})
	if err != nil {
		return u, err
	}

	// The code is spent: do not leave it readable in the data directory.
	if a.Local && a.dataDir != "" {
		if err := os.Remove(a.LocalResetFile()); err != nil && !errors.Is(err, fs.ErrNotExist) {
			a.log.ErrorContext(ctx, "reset: could not remove the spent code file", "err", err)
		}
	}

	// After the commit: the session store may share SQLite's one writer.
	return u, a.revokeServerSessions(ctx, u.ID, "")
}

func digest(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
