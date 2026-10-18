package requests_test

import (
	"strings"
	"testing"

	"github.com/AlexGalhardo/vukem-tools/app/requests"
)

// The policy is the one the client mirrors, so each class is asserted
// separately: a change that silently drops one would otherwise pass.
func TestValidatePassword(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		password string
		missing  []string
	}{
		"all four classes": {"Abcdefg1!", nil},
		"accented letters": {"Ábcdefg1!", nil},
		"no upper":         {"abcdefg1!", []string{"an upper-case letter"}},
		"no lower":         {"ABCDEFG1!", []string{"a lower-case letter"}},
		"no digit":         {"Abcdefgh!", []string{"a digit"}},
		"no special":       {"Abcdefg1", []string{"a special character"}},
		"only lower":       {"abcdefgh", []string{"an upper-case letter", "a digit", "a special character"}},
		"empty": {
			"",
			[]string{"an upper-case letter", "a lower-case letter", "a digit", "a special character"},
		},
		"space is not special": {"Abcdefg 1", []string{"a special character"}},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			err := requests.ValidatePassword(tc.password)

			if len(tc.missing) == 0 {
				if err != nil {
					t.Fatalf("ValidatePassword(%q) = %v, want nil", tc.password, err)
				}

				return
			}

			if err == nil {
				t.Fatalf("ValidatePassword(%q) = nil, want it to report %v", tc.password, tc.missing)
			}

			for _, want := range tc.missing {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("ValidatePassword(%q) = %q, want it to mention %q", tc.password, err, want)
				}
			}
		})
	}
}

// Length is the schema's job, so the class check must not also reject a short
// password: the user would be told the wrong thing.
func TestValidatePasswordIgnoresLength(t *testing.T) {
	t.Parallel()

	if err := requests.ValidatePassword("Ab1!"); err != nil {
		t.Errorf("ValidatePassword on a short but complete password = %v, want nil", err)
	}

	if requests.PasswordMinLength != 8 || requests.PasswordMaxLength != 64 {
		t.Errorf("policy bounds are %d..%d; resources/js/components/PasswordStrength.tsx must match",
			requests.PasswordMinLength, requests.PasswordMaxLength)
	}
}
