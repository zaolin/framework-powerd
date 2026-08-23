package detector

import (
	"testing"
)

// TestParseStatLine_Valid verifies T5: parseStatLine extracts PPID and name
// from a real /proc/<pid>/stat formatted string.
func TestParseStatLine_Valid(t *testing.T) {
	stat := "12345 (steam.exe) S 1 0 0 0 -1 0 0 0 0 0"
	ppid, name, err := parseStatLine(stat)
	if err != nil {
		t.Fatalf("parseStatLine error: %v", err)
	}
	if ppid != 1 {
		t.Errorf("ppid = %d, want 1", ppid)
	}
	if name != "steam.exe" {
		t.Errorf("name = %q, want %q", name, "steam.exe")
	}
}

// TestParseStatLine_ParensInName verifies the parser handles a comm field
// containing parentheses — e.g., "weird (name) here".
func TestParseStatLine_ParensInName(t *testing.T) {
	stat := "123 (weird (name) here) S 456 0 0 0"
	ppid, name, err := parseStatLine(stat)
	if err != nil {
		t.Fatalf("parseStatLine error: %v", err)
	}
	if name != "weird (name) here" {
		t.Errorf("name = %q, want %q", name, "weird (name) here")
	}
	if ppid != 456 {
		t.Errorf("ppid = %d, want 456", ppid)
	}
}

// TestParseStatLine_Malformed verifies the parser returns errors for bad input.
func TestParseStatLine_Malformed(t *testing.T) {
	cases := []string{
		"",
		"no parens here",
		"(no pid prefix)",
		"123 () S",  // too few fields after the paren
	}
	for _, c := range cases {
		_, _, err := parseStatLine(c)
		if err == nil {
			t.Errorf("expected error for malformed input %q, got nil", c)
		}
	}
}

// TestEnvironHasSteamAppId_Found verifies T6: environHasSteamAppId correctly
// finds a SteamAppId= entry with a value.
func TestEnvironHasSteamAppId_Found(t *testing.T) {
	content := []byte("PATH=/usr/bin\x00SteamAppId=12345\x00HOME=/home\x00")
	if !environHasSteamAppId(content) {
		t.Error("expected environHasSteamAppId to return true")
	}
}

// TestEnvironHasSteamAppId_AtStart verifies it works when SteamAppId is the
// first entry (boundary check: index 0).
func TestEnvironHasSteamAppId_AtStart(t *testing.T) {
	content := []byte("SteamAppId=12345\x00PATH=/usr/bin\x00")
	if !environHasSteamAppId(content) {
		t.Error("expected environHasSteamAppId to return true at index 0")
	}
}

// TestEnvironHasSteamAppId_NotFound verifies it returns false when the key is
// absent.
func TestEnvironHasSteamAppId_NotFound(t *testing.T) {
	content := []byte("PATH=/usr/bin\x00HOME=/home\x00")
	if environHasSteamAppId(content) {
		t.Error("expected environHasSteamAppId to return false")
	}
}

// TestEnvironHasSteamAppId_EmptyValue verifies it rejects an empty value
// (SteamAppId=\0).
func TestEnvironHasSteamAppId_EmptyValue(t *testing.T) {
	content := []byte("SteamAppId=\x00PATH=/usr/bin\x00")
	if environHasSteamAppId(content) {
		t.Error("expected environHasSteamAppId to return false for empty value")
	}
}

// TestEnvironHasSteamAppId_FakeKey verifies the boundary check rejects keys
// like "FakeSteamAppId=" that merely contain the substring.
func TestEnvironHasSteamAppId_FakeKey(t *testing.T) {
	content := []byte("FakeSteamAppId=123\x00PATH=/usr/bin\x00")
	if environHasSteamAppId(content) {
		t.Error("expected environHasSteamAppId to return false for FakeSteamAppId=")
	}
}