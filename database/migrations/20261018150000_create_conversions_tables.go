package migrations

import "github.com/AlexGalhardo/vukem/schema"

// The app's own job record and its progress trail.
//
// They are not called "jobs" and "job_events": vukem/queue already owns a
// table named "jobs" (and "failed_jobs"), which it creates at boot. A
// conversion is the domain object the user sees and polls; the queue row is
// the framework's internal unit of work for it.
func m20261018150000CreateConversionsTables() schema.Migration {
	return schema.Migration{
		ID: "20261018150000_create_conversions_tables",
		Up: func(s *schema.Schema) error {
			if err := s.Create("conversions", func(t *schema.Table) {
				t.ULID("id").Unique()
				t.String("user_id")
				// Which tool produced it, e.g. "pixels-to-svg", "image-convert".
				t.String("tool")
				t.Enum("status", "pending", "running", "finished", "error", "cancelled").Default("pending")
				// 0 to 100. Written by the worker, read by the SSE stream.
				t.Integer("progress").Default(0)
				// Human-readable stage, e.g. "downloading", "encoding".
				t.String("phase").Default("")
				t.BigInteger("bytes_in").Default(0)
				t.BigInteger("bytes_out").Default(0)
				// Measured CPU time, which feeds the per-user daily quota.
				t.BigInteger("cpu_ms").Default(0)
				// The validated tool options, as sent.
				t.JSON("options")
				// Stable code from the app's error taxonomy; the detail is for
				// the owner, never echoed verbatim to the client.
				t.String("error_type").Nullable()
				t.Text("error_detail").Nullable()
				// Set when the caller supplied an idempotency key.
				t.String("idempotency_key").Nullable()
				t.Timestamp("started_at").Nullable()
				t.Timestamp("finished_at").Nullable()
				// Results are deleted an hour after they are produced.
				t.Timestamp("expires_at")
				t.Timestamps()

				// "list my conversions, newest first" on the account page.
				t.Index("user_id", "created_at")
				// The same input plus the same options returns the existing
				// conversion instead of starting a second one.
				t.UniqueIndex("user_id", "idempotency_key")
				// The scheduled cleanup scans for expired rows.
				t.Index("expires_at")
			}); err != nil {
				return err
			}

			return s.Create("conversion_events", func(t *schema.Table) {
				t.ID()
				t.ULID("conversion_id")
				t.Enum("status", "pending", "running", "finished", "error", "cancelled")
				t.Integer("progress").Default(0)
				t.String("phase").Default("")
				t.Text("message").Default("")
				t.Timestamp("created_at")

				// Replaying one conversion's trail in order. Covers lookups by
				// conversion_id alone, so no separate index on it.
				t.Index("conversion_id", "created_at")
			})
		},
		Down: func(s *schema.Schema) error {
			for _, table := range []string{"conversion_events", "conversions"} {
				if err := s.DropIfExists(table); err != nil {
					return err
				}
			}

			return nil
		},
	}
}
