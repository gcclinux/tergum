package identity

import (
	"runtime"
	"testing"
)

func TestGetSystemIdentity(t *testing.T) {
	id := GetSystemIdentity()

	if id.Hostname == "" {
		t.Error("expected non-empty hostname")
	}
	if id.OSFamily != runtime.GOOS {
		t.Errorf("expected OSFamily %q, got %q", runtime.GOOS, id.OSFamily)
	}
	if id.MachineID == "" {
		t.Error("expected non-empty MachineID")
	}

	// Calling it again should be deterministic on the same machine
	id2 := GetSystemIdentity()
	if id.MachineID != id2.MachineID {
		t.Errorf("MachineID not deterministic: %q vs %q", id.MachineID, id2.MachineID)
	}
}

func TestCheckOSCompatibility(t *testing.T) {
	tests := []struct {
		stored   string
		current  string
		wantErr  bool
	}{
		{"linux", "linux", false},
		{"windows", "windows", false},
		{"darwin", "darwin", false},
		{"", "linux", false},
		{"linux", "", false},
		{"windows", "linux", true},
		{"linux", "windows", true},
		{"windows", "darwin", true},
		{"darwin", "linux", true},
	}

	for _, tt := range tests {
		err := CheckOSCompatibility(tt.stored, tt.current)
		if (err != nil) != tt.wantErr {
			t.Errorf("CheckOSCompatibility(%q, %q) error = %v, wantErr %v", tt.stored, tt.current, err, tt.wantErr)
		}
	}
}
