package migrations

import "github.com/AlexGalhardo/vukem/schema"

// One row per file the app holds: every upload it accepted and every result it
// produced. The bytes live in object storage; this table is the index over
// them, and the only thing the cleanup needs to read.
func m20261018150001CreateFilesTable() schema.Migration {
	return schema.Migration{
		ID: "20261018150001_create_files_table",
		Up: func(s *schema.Schema) error {
			return s.Create("files", func(t *schema.Table) {
				t.ULID("id").Unique()
				t.String("user_id")
				// Null while an upload has not been attached to a conversion yet.
				t.ULID("conversion_id").Nullable()
				t.Enum("role", "input", "output")
				// Key in the bucket. Unique so a key can never be claimed twice.
				t.String("disk_key").Unique()
				// The sanitised name shown to the user on download.
				t.String("name")
				// The type detected from the file's magic bytes, never from its
				// extension or from the client's Content-Type.
				t.String("mime")
				t.BigInteger("size_bytes")
				t.String("checksum").Nullable()
				t.Timestamp("expires_at")
				t.Timestamp("created_at")

				// "the inputs of this conversion", "the outputs of this conversion".
				t.Index("conversion_id", "role")
				// "list my files, newest first" on the account page.
				t.Index("user_id", "created_at")
				// The scheduled cleanup scans for expired rows to delete from
				// the bucket.
				t.Index("expires_at")
			})
		},
		Down: func(s *schema.Schema) error { return s.DropIfExists("files") },
	}
}
