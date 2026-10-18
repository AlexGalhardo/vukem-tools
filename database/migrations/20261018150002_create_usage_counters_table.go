package migrations

import "github.com/AlexGalhardo/vukem/schema"

// What each user spent, per day. The quota in the rate-limiting layer is
// measured in CPU milliseconds rather than in a file count, because a 2 KB SVG
// and a 500 MB video cost nothing alike.
func m20261018150002CreateUsageCountersTable() schema.Migration {
	return schema.Migration{
		ID: "20261018150002_create_usage_counters_table",
		Up: func(s *schema.Schema) error {
			return s.Create("usage_counters", func(t *schema.Table) {
				t.ID()
				t.String("user_id")
				t.Date("day")
				t.BigInteger("cpu_ms").Default(0)
				t.BigInteger("bytes_in").Default(0)
				t.BigInteger("bytes_out").Default(0)
				t.Integer("conversions").Default(0)
				t.Timestamps()

				// One row per user per day, and the key the quota check reads
				// before admitting a conversion.
				t.UniqueIndex("user_id", "day")
			})
		},
		Down: func(s *schema.Schema) error { return s.DropIfExists("usage_counters") },
	}
}
