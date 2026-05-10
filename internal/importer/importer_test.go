package importer

import (
	"strings"
	"testing"
)

func TestFormatCommand(t *testing.T) {
	tests := []struct {
		name        string
		raw         string
		wantCommand string
		wantTSUnix  *int64
		wantDur     *int
	}{
		{
			name:        "well-formed extended history line",
			raw:         ": 1700000000:5;git status",
			wantCommand: "git status",
			wantTSUnix:  ptrInt64(1700000000),
			wantDur:     ptrInt(5),
		},
		{
			name:        "plain command with no metadata prefix",
			raw:         "ls",
			wantCommand: "ls",
			wantTSUnix:  nil,
			wantDur:     nil,
		},
		{
			name:        "malformed timestamp falls back to raw line",
			raw:         ": notanumber:5;ls",
			wantCommand: ": notanumber:5;ls",
			wantTSUnix:  nil,
			wantDur:     nil,
		},
		{
			name:        "malformed duration falls back to raw line",
			raw:         ": 1700000000:abc;ls",
			wantCommand: ": 1700000000:abc;ls",
			wantTSUnix:  nil,
			wantDur:     nil,
		},
		{
			name:        "command containing semicolons preserves trailing segments",
			raw:         ": 1700000000:0;echo hi; echo bye",
			wantCommand: "echo hi; echo bye",
			wantTSUnix:  ptrInt64(1700000000),
			wantDur:     ptrInt(0),
		},
		{
			name:        "empty command after delimiter",
			raw:         ": 1700000000:0;",
			wantCommand: "",
			wantTSUnix:  ptrInt64(1700000000),
			wantDur:     ptrInt(0),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatCommand(tt.raw)

			if got.Command != tt.wantCommand {
				t.Errorf("Command = %q, want %q", got.Command, tt.wantCommand)
			}

			switch {
			case tt.wantTSUnix == nil && got.Timestamp != nil:
				t.Errorf("Timestamp = %v, want nil", got.Timestamp)
			case tt.wantTSUnix != nil && got.Timestamp == nil:
				t.Errorf("Timestamp = nil, want unix=%d", *tt.wantTSUnix)
			case tt.wantTSUnix != nil && got.Timestamp.Unix() != *tt.wantTSUnix:
				t.Errorf("Timestamp.Unix() = %d, want %d", got.Timestamp.Unix(), *tt.wantTSUnix)
			}

			switch {
			case tt.wantDur == nil && got.Duration != nil:
				t.Errorf("Duration = %v, want nil", got.Duration)
			case tt.wantDur != nil && got.Duration == nil:
				t.Errorf("Duration = nil, want %d", *tt.wantDur)
			case tt.wantDur != nil && *got.Duration != *tt.wantDur:
				t.Errorf("Duration = %d, want %d", *got.Duration, *tt.wantDur)
			}
		})
	}
}

func TestParseHistoryCommand(t *testing.T) {
	t.Run("empty input yields no entries", func(t *testing.T) {
		if got := parseHistoryCommand(""); len(got) != 0 {
			t.Errorf("len(got) = %d, want 0: %+v", len(got), got)
		}
	})

	t.Run("filters blank and whitespace-only lines", func(t *testing.T) {
		input := strings.Join([]string{
			": 1700000000:0;ls",
			"",
			"   ",
			"\t",
			": 1700000001:0;pwd",
		}, "\n")

		got := parseHistoryCommand(input)
		if len(got) != 2 {
			t.Fatalf("len(got) = %d, want 2: %+v", len(got), got)
		}
		if got[0].Command != "ls" || got[1].Command != "pwd" {
			t.Errorf("commands = [%q, %q], want [ls, pwd]", got[0].Command, got[1].Command)
		}
	})

	t.Run("trailing newline does not produce a phantom entry", func(t *testing.T) {
		got := parseHistoryCommand(": 1700000000:0;ls\n")
		if len(got) != 1 {
			t.Fatalf("len(got) = %d, want 1: %+v", len(got), got)
		}
		if got[0].Command != "ls" {
			t.Errorf("Command = %q, want ls", got[0].Command)
		}
	})

	t.Run("mixes valid and plain lines", func(t *testing.T) {
		input := ": 1700000000:0;git status\nls\n: 1700000001:2;go test"
		got := parseHistoryCommand(input)
		if len(got) != 3 {
			t.Fatalf("len(got) = %d, want 3: %+v", len(got), got)
		}
		if got[0].Timestamp == nil || got[1].Timestamp != nil || got[2].Timestamp == nil {
			t.Errorf("expected timestamps on entries 0 and 2, none on 1; got %+v", got)
		}
	})
}

func ptrInt64(v int64) *int64 { return &v }
func ptrInt(v int) *int       { return &v }
