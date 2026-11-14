package routes

import "testing"

func TestScannerPolicy(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		production bool
		addr       string
		disabled   bool
		wantScan   bool
		wantErr    bool
	}{
		{name: "production without clamd refuses to boot", production: true, wantErr: true},
		{name: "production with clamd scans", production: true, addr: "clamd:3310", wantScan: true},
		{name: "production opted out on purpose", production: true, disabled: true},
		{
			name:       "the opt-out never beats a configured scanner",
			production: true,
			addr:       "clamd:3310",
			disabled:   true,
			wantScan:   true,
		},
		{name: "development without clamd is allowed", production: false},
		{name: "development with clamd scans", addr: "127.0.0.1:3310", wantScan: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			scanner, err := scannerFor(tc.production, tc.addr, tc.disabled)
			if (err != nil) != tc.wantErr {
				t.Fatalf("error = %v, want error: %v", err, tc.wantErr)
			}

			if (scanner != nil) != tc.wantScan {
				t.Fatalf("scanner present = %v, want %v", scanner != nil, tc.wantScan)
			}
		})
	}
}
