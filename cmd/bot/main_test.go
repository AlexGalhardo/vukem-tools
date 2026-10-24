package main

import (
	"slices"
	"testing"
)

// The conversation, the allow-list check and the API client are tested in
// internal/bot, with Telegram stubbed. What is left in this package is the
// parsing of the one setting that decides who may use the bot, which is
// exactly the piece worth a test of its own: a mistake here is an open bot.

func TestTheAllowListIsParsedFromTheSetting(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		raw  string
		want []int64
	}{
		{name: "one id", raw: "4242", want: []int64{4242}},
		{name: "several", raw: "1,2,3", want: []int64{1, 2, 3}},
		{name: "spaces around them", raw: " 1 , 2 ,3 ", want: []int64{1, 2, 3}},
		{name: "a trailing comma", raw: "1,2,", want: []int64{1, 2}},
		// Telegram ids are int64 and real ones are already past int32.
		{name: "a large id", raw: "7846321905", want: []int64{7846321905}},
		// Telegram uses negative ids for chats, which are not users, but
		// parsing them is not this function's business to refuse.
		{name: "a negative id", raw: "-100123", want: []int64{-100123}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			got, err := allowedUsers(c.raw)
			if err != nil {
				t.Fatalf("allowedUsers(%q): %v", c.raw, err)
			}

			if !slices.Equal(got, c.want) {
				t.Fatalf("allowedUsers(%q) = %v, want %v", c.raw, got, c.want)
			}
		})
	}
}

func TestAnEmptyAllowListMeansAnybodyMayTrySigningIn(t *testing.T) {
	t.Parallel()

	// The sign-in is the gate now: a stranger who signs in reaches only their
	// own account. The list restricts who may use the bot's capacity at all,
	// and leaving it empty is a choice, not a mistake.
	for _, raw := range []string{"", "   ", ",", " , , "} {
		ids, err := allowedUsers(raw)
		if err != nil || len(ids) != 0 {
			t.Errorf("allowedUsers(%q) = %v, %v; want no ids and no error", raw, ids, err)
		}
	}
}

func TestANonNumericOrZeroIdIsRefused(t *testing.T) {
	t.Parallel()

	cases := []string{
		"alice",
		"4242,alice",
		"42.5",
		"0x10",
		// Zero is what an update with no sender would compare as, so it must
		// never be allowed by configuration: bot.Allows refuses a zero
		// sender, and a zero in the list would be a second place that has to
		// agree with it.
		"0",
		"4242,0",
	}

	for _, raw := range cases {
		if _, err := allowedUsers(raw); err == nil {
			t.Errorf("allowedUsers(%q) was accepted", raw)
		}
	}
}

func TestTheRefusalNamesTheSettingAndTheOffendingValue(t *testing.T) {
	t.Parallel()

	// An operator reads this in a crash log with no other context, so it has
	// to say which setting and which value.
	_, err := allowedUsers("4242,alice")
	if err == nil {
		t.Fatal("a non-numeric id was accepted")
	}

	for _, want := range []string{"TELEGRAM_ALLOWED_USERS", "alice"} {
		if !contains(err.Error(), want) {
			t.Errorf("the error does not mention %q: %v", want, err)
		}
	}
}

func contains(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}

	return false
}
