package quota

import (
	"database/sql"
	"errors"
	"fmt"
	"testing"
)

// A driver that wraps sql.ErrNoRows, or rewords it, must still read as "no
// usage today": comparing the message would turn that into a hard failure of
// every conversion.
func TestIsNoRowsFollowsTheErrorChain(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"the sentinel", sql.ErrNoRows, true},
		{"wrapped", fmt.Errorf("scan: %w", sql.ErrNoRows), true},
		{"joined", errors.Join(errors.New("context"), sql.ErrNoRows), true},
		{"the same words without the sentinel", errors.New("sql: no rows in result set"), false},
		{"another error", errors.New("connection refused"), false},
		{"nil", nil, false},
	}

	for _, tc := range tests {
		if got := isNoRows(tc.err); got != tc.want {
			t.Errorf("%s: isNoRows = %v, want %v", tc.name, got, tc.want)
		}
	}
}
