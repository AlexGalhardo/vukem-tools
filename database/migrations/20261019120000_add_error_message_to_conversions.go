package migrations

import "github.com/AlexGalhardo/vukem/schema"

// Adds the client-safe failure message to the conversions table.
//
// Before this, a failure stored only `error_type` and the owner-only
// `error_detail`, and the API derived what to show from the type alone. That
// threw away the specific sentence the code that detected the problem had
// written: jobs.Refuse exists precisely so a cap can be reported as "that
// archive holds more than 64 files" rather than "that file is past the limit
// for this tool", and a wrong PDF password as itself rather than as a generic
// unsupported input. The sentence was recorded on the progress trail, so a
// client watching the live stream saw it and a client that reloaded the page
// did not — two surfaces wording the same failure differently, which is the
// one thing jobs.MessageOf exists to prevent.
//
// Nullable, with no backfill: a conversion that failed before this migration
// has no sentence to recover, and the API falls back to the taxonomy's
// message, which is what it was already showing.
func m20261019120000AddErrorMessageToConversions() schema.Migration {
	return schema.Migration{
		ID: "20261019120000_add_error_message_to_conversions",
		Up: func(s *schema.Schema) error {
			return s.Table("conversions", func(t *schema.Table) {
				t.Text("error_message").Nullable()
			})
		},
		Down: func(s *schema.Schema) error {
			return s.Table("conversions", func(t *schema.Table) {
				t.DropColumn("error_message")
			})
		},
	}
}
