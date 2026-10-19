package migrations

import "github.com/AlexGalhardo/vukem/schema"

// Adds the conversion's own account of what it did.
//
// Several tools produce a result whose value is not obvious from the file: a
// compression that could not beat the original returns it unchanged, a split
// says how many frames it cut and how many it dropped as noise, a build says
// how much of the packed sheet is used. That sentence was being written to the
// progress trail only, which made it a race: a conversion that finishes before
// the browser's event stream connects is answered with a snapshot of the
// final state, and the snapshot carries no trail. The fast conversions — which
// is most of them — therefore lost the one thing that explained the result.
//
// Nullable, because most conversions have nothing to add beyond the file they
// produced, and because a conversion that ran before this column existed has
// no sentence to recover.
func m20261019150000AddNoteToConversions() schema.Migration {
	return schema.Migration{
		ID: "20261019150000_add_note_to_conversions",
		Up: func(s *schema.Schema) error {
			return s.Table("conversions", func(t *schema.Table) {
				t.Text("note").Nullable()
			})
		},
		Down: func(s *schema.Schema) error {
			return s.Table("conversions", func(t *schema.Table) {
				t.DropColumn("note")
			})
		},
	}
}
