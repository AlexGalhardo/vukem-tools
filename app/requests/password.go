package requests

import (
	"strings"
	"unicode"

	"github.com/AlexGalhardo/vukem/z"
)

// The password policy, in one place. The client mirrors it in
// resources/js/components/PasswordStrength.tsx, so a change here is a change
// there: a form that promises different rules than the server enforces is
// worse than no hint at all.
const (
	// PasswordMinLength is the shortest password accepted.
	PasswordMinLength = 8
	// PasswordMaxLength is the longest. A cap belongs on any input that
	// reaches a hashing function, because Argon2id cost grows with length.
	PasswordMaxLength = 64
)

// classes reports which of the four required character classes a password has.
// Unicode-aware on purpose: "Ä" is an upper-case letter and "ß" a lower-case
// one, and an ASCII-only check would refuse a perfectly good password.
//
// A special character is anything that is not a letter, not a digit and not
// whitespace. Whitespace is allowed — passphrases are good — but it does not
// satisfy the special-character requirement, because a trailing space is not a
// deliberate choice.
func classes(password string) (upper, lower, digit, special bool) {
	for _, r := range password {
		switch {
		case unicode.IsUpper(r):
			upper = true
		case unicode.IsLower(r):
			lower = true
		case unicode.IsDigit(r):
			digit = true
		case !unicode.IsSpace(r) && !unicode.IsLetter(r):
			special = true
		}
	}

	return upper, lower, digit, special
}

// ValidatePassword reports every requirement the password fails to meet, in
// one error, so the user is not sent round the loop once per rule. Length is
// left to the schema's Min and Max, which produce their own messages.
func ValidatePassword(password string) error {
	upper, lower, digit, special := classes(password)

	var missing []string

	if !upper {
		missing = append(missing, "an upper-case letter")
	}

	if !lower {
		missing = append(missing, "a lower-case letter")
	}

	if !digit {
		missing = append(missing, "a digit")
	}

	if !special {
		missing = append(missing, "a special character")
	}

	if len(missing) == 0 {
		return nil
	}

	return z.FieldError("password", "The password needs "+join(missing)+".")
}

// join renders a list as "a, b and c", which reads better in a message than a
// comma-separated tail.
func join(parts []string) string {
	switch len(parts) {
	case 1:
		return parts[0]
	case 2:
		return parts[0] + " and " + parts[1]
	default:
		return strings.Join(parts[:len(parts)-1], ", ") + " and " + parts[len(parts)-1]
	}
}
