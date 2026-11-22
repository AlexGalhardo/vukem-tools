package accounts

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"net/url"
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

// LocalResetCode issues the single-use reset code of the account with email and
// returns it, for the local answer to show instead of mailing it: a local
// machine has no mailbox. found is false for an unknown email, and no code
// exists then. Anyone who can reach the loopback server can ask, which is no
// more than they could do with the database file; the code ends the account's
// sessions and keys when used, like the mailed one (ResetPassword). It exists
// only in local mode.
func (a *Accounts) LocalResetCode(ctx context.Context, email string) (code string, found bool, err error) {
	if !a.Local {
		return "", false, errors.New("accounts: reset codes are issued in local mode only")
	}

	email = strings.ToLower(strings.TrimSpace(email))

	_, err = a.Users.ByCredentials(ctx, map[string]string{"email": email})
	if errors.Is(err, auth.ErrUserNotFound) {
		return "", false, nil
	}

	if err != nil {
		return "", false, err
	}

	code, err = a.storeResetToken(ctx, email)

	return code, err == nil, err
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
	if err != nil || subtle.ConstantTimeCompare([]byte(stored), []byte(digest(token))) != 1 ||
		a.expired(created, a.ResetTTL) {
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

	// After the commit: the session store may share SQLite's one writer.
	return u, a.revokeServerSessions(ctx, u.ID, "")
}

func digest(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
