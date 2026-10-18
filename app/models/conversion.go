package models

import (
	"encoding/json"
	"time"

	"github.com/AlexGalhardo/vukem/orm"
)

// Conversion is one unit of work the user submitted: which tool, with which
// options, how far it got and how it ended.
//
// The primary key is a ULID rather than an auto-increment integer. It is
// handed to clients and appears in URLs, so a sequential key would leak how
// many conversions the service has run and let anyone walk the list; a ULID
// keeps "newest first" in the key itself, which a random UUID would not.
// Because the key is a string, orm.Create does not generate it:
// internal/jobs sets it from internal/ids.
type Conversion struct {
	ID     string `db:"id"      json:"id"`
	UserID string `db:"user_id" json:"-"`
	Tool   string `db:"tool"    json:"tool"`
	Status string `db:"status"  json:"status"`

	Progress int    `db:"progress" json:"progress"`
	Phase    string `db:"phase"    json:"phase"`

	BytesIn  int64 `db:"bytes_in"  json:"bytes_in"`
	BytesOut int64 `db:"bytes_out" json:"bytes_out"`
	CPUMs    int64 `db:"cpu_ms"    json:"cpu_ms"`

	// Options is the validated tool input, stored exactly as it was accepted,
	// so a result can be explained and a repeat submission compared.
	Options orm.JSON[json.RawMessage] `db:"options" json:"options"`

	// Nullable columns are plain pointers: orm.NullString does not exist, and
	// sql.Null[string] serialises as {"V":...,"Valid":...}, which is not a
	// shape any client should have to read.
	//
	// ErrorType is a code from the jobs taxonomy. ErrorDetail is for the
	// owner only: it may hold a path or an engine command line, so it is
	// never serialised to a client, which is what the "-" tag enforces.
	ErrorType   *string `db:"error_type"   json:"error_type"`
	ErrorDetail *string `db:"error_detail" json:"-"`

	// IdempotencyKey is set when the caller supplied one. Unique per user, so
	// a repeated submission returns this row instead of starting a second
	// conversion.
	IdempotencyKey *string `db:"idempotency_key" json:"-"`

	StartedAt  orm.NullTime `db:"started_at"  json:"started_at"`
	FinishedAt orm.NullTime `db:"finished_at" json:"finished_at"`
	// ExpiresAt is when the result stops being downloadable and the cleanup
	// may delete it.
	ExpiresAt time.Time `db:"expires_at" json:"expires_at"`

	CreatedAt time.Time `db:"created_at" json:"created_at"`
	UpdatedAt time.Time `db:"updated_at" json:"updated_at"`
}

// Table implements orm.Tabler.
func (Conversion) Table() string { return "conversions" }

// PrimaryKey implements orm.Keyer. Without it the ORM would treat the string
// id as an auto-increment column and ask the database to generate it.
func (Conversion) PrimaryKey() string { return "id" }

// ConversionEvent is one line of a conversion's progress trail: what the
// worker reported and when. The trail is what a client that reconnects to the
// progress stream replays, and what explains after the fact how a conversion
// reached its end state.
type ConversionEvent struct {
	ID           int64  `db:"id"            json:"-"`
	ConversionID string `db:"conversion_id" json:"-"`
	Status       string `db:"status"        json:"status"`
	Progress     int    `db:"progress"      json:"progress"`
	Phase        string `db:"phase"         json:"phase"`
	// Message is shown to the owner of the conversion, so it carries no path
	// and no engine argument — only the taxonomy's client-safe text.
	Message   string    `db:"message"    json:"message"`
	CreatedAt time.Time `db:"created_at" json:"at"`
}

// Table implements orm.Tabler.
func (ConversionEvent) Table() string { return "conversion_events" }

// File is one file the app holds: an upload it accepted or a result it
// produced. The bytes live in object storage; this row is the index over them.
type File struct {
	ID           string  `db:"id"            json:"id"`
	UserID       string  `db:"user_id"       json:"-"`
	ConversionID *string `db:"conversion_id" json:"-"`
	Role         string  `db:"role"          json:"role"`
	// DiskKey is the key in the bucket. Never sent to a client: a download
	// goes through a signed URL, so the key itself stays private.
	DiskKey   string  `db:"disk_key"   json:"-"`
	Name      string  `db:"name"       json:"name"`
	MIME      string  `db:"mime"       json:"mime"`
	SizeBytes int64   `db:"size_bytes" json:"size_bytes"`
	Checksum  *string `db:"checksum"   json:"checksum"`

	ExpiresAt time.Time `db:"expires_at" json:"expires_at"`
	CreatedAt time.Time `db:"created_at" json:"created_at"`
}

// Table implements orm.Tabler.
func (File) Table() string { return "files" }

// PrimaryKey implements orm.Keyer.
func (File) PrimaryKey() string { return "id" }

// The two roles a file can have.
const (
	// FileInput is a file the user uploaded.
	FileInput = "input"
	// FileOutput is a file a conversion produced.
	FileOutput = "output"
)
