package importer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadHistory(t *testing.T) {
	const sample = ": 1700000000:0;echo hello\n: 1700000001:0;git status\n"

	t.Run("explicit path is read directly", func(t *testing.T) {
		dir := t.TempDir()
		p := filepath.Join(dir, "custom_history")
		if err := os.WriteFile(p, []byte(sample), 0o644); err != nil {
			t.Fatal(err)
		}
		// An explicit path must win over HISTFILE and the home default.
		t.Setenv("HISTFILE", filepath.Join(dir, "should_not_be_used"))

		got, err := ReadHistory(p)
		if err != nil {
			t.Fatalf("ReadHistory(%q) error: %v", p, err)
		}
		if len(got) != 2 || got[0].Command != "echo hello" || got[1].Command != "git status" {
			t.Fatalf("got %+v, want [echo hello, git status]", got)
		}
	})

	t.Run("empty path falls back to HISTFILE", func(t *testing.T) {
		dir := t.TempDir()
		p := filepath.Join(dir, "histfile")
		if err := os.WriteFile(p, []byte(sample), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Setenv("HISTFILE", p)

		got, err := ReadHistory("")
		if err != nil {
			t.Fatalf(`ReadHistory("") error: %v`, err)
		}
		if len(got) != 2 {
			t.Fatalf("len(got) = %d, want 2: %+v", len(got), got)
		}
	})

	t.Run("empty path and no HISTFILE falls back to ~/.zsh_history", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		t.Setenv("HISTFILE", "")
		if err := os.WriteFile(filepath.Join(home, ".zsh_history"), []byte(sample), 0o644); err != nil {
			t.Fatal(err)
		}

		got, err := ReadHistory("")
		if err != nil {
			t.Fatalf(`ReadHistory("") error: %v`, err)
		}
		if len(got) != 2 {
			t.Fatalf("len(got) = %d, want 2: %+v", len(got), got)
		}
	})

	t.Run("missing file returns an error", func(t *testing.T) {
		if _, err := ReadHistory(filepath.Join(t.TempDir(), "does_not_exist")); err == nil {
			t.Fatal("expected error for missing file, got nil")
		}
	})
}

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

	// zsh stores an embedded newline by prepending its own escape backslash, so
	// a physical line ending in `\` is a continuation of the next line. The real
	// histfile bytes look like `...config \\<NL>   -type f \\<NL>...` — one
	// backslash from the user's shell continuation, one added by zsh. The parser
	// must strip exactly the zsh escape and rejoin with a real `\n` so a single
	// multiline command stays one entry (with the user's own `\` preserved).
	t.Run("backslash continuation joins into one multiline command", func(t *testing.T) {
		input := strings.Join([]string{
			`: 1700000000:0;find ~/.config \\`,
			`   -type f \\`,
			`   -name '*.json'`,
		}, "\n")

		want := strings.Join([]string{
			`find ~/.config \`,
			`   -type f \`,
			`   -name '*.json'`,
		}, "\n")

		got := parseHistoryCommand(input)
		if len(got) != 1 {
			t.Fatalf("len(got) = %d, want 1: %+v", len(got), got)
		}
		if got[0].Command != want {
			t.Errorf("Command = %q, want %q", got[0].Command, want)
		}
		if got[0].Timestamp == nil || got[0].Timestamp.Unix() != 1700000000 {
			t.Errorf("Timestamp = %v, want unix=1700000000", got[0].Timestamp)
		}
	})

	t.Run("multiline entry followed by a normal entry yields two commands", func(t *testing.T) {
		input := strings.Join([]string{
			`: 1700000000:0;find ~/.config \\`,
			`   -type f`,
			`: 1700000001:0;ls`,
		}, "\n")

		wantFirst := strings.Join([]string{
			`find ~/.config \`,
			`   -type f`,
		}, "\n")

		got := parseHistoryCommand(input)
		if len(got) != 2 {
			t.Fatalf("len(got) = %d, want 2: %+v", len(got), got)
		}
		if got[0].Command != wantFirst {
			t.Errorf("got[0].Command = %q, want %q", got[0].Command, wantFirst)
		}
		if got[1].Command != "ls" {
			t.Errorf("got[1].Command = %q, want %q", got[1].Command, "ls")
		}
	})
}

func ptrInt64(v int64) *int64 { return &v }
func ptrInt(v int) *int       { return &v }

// A history file written while HIST_IGNORE_SPACE was off can still contain
// space-prefixed lines. Import drops them so the DB never gains a command the
// live hooks would refuse to record.
func TestParseHistoryCommand_DropsIgnoredCommands(t *testing.T) {
	t.Run("extended history hides the space behind the metadata prefix", func(t *testing.T) {
		input := strings.Join([]string{
			": 1700000000:0;git status",
			": 1700000001:0; export AWS_SECRET=hunter2",
			": 1700000002:0;\texport OTHER_SECRET=x",
			": 1700000003:0;git push",
		}, "\n")

		got := parseHistoryCommand(input)
		if len(got) != 2 {
			t.Fatalf("len(got) = %d, want 2: %+v", len(got), got)
		}
		if got[0].Command != "git status" || got[1].Command != "git push" {
			t.Errorf("commands = [%q, %q], want [git status, git push]", got[0].Command, got[1].Command)
		}
	})

	t.Run("plain history lines", func(t *testing.T) {
		got := parseHistoryCommand("ls\n export SECRET=x\npwd")
		if len(got) != 2 {
			t.Fatalf("len(got) = %d, want 2: %+v", len(got), got)
		}
		for _, c := range got {
			if strings.Contains(c.Command, "SECRET") {
				t.Errorf("ignored command survived import: %q", c.Command)
			}
		}
	})
}
