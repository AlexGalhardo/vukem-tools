package migrations

import "github.com/AlexGalhardo/vukem/schema"

func m20261018160530CreateSocialAccountsTable() schema.Migration {
	return schema.Migration{
		ID: "20261018160530_create_social_accounts_table",
		Up: func(s *schema.Schema) error {
			return s.Create("social_accounts", func(t *schema.Table) {
				t.ID()
				t.ForeignID("user_id").Constrained("users").CascadeOnDelete()
				t.String("provider")
				t.String("provider_id")
				t.Timestamps()
				t.UniqueIndex("provider", "provider_id")
			})
		},
		Down: func(s *schema.Schema) error { return s.DropIfExists("social_accounts") },
	}
}
