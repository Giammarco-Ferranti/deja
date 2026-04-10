package importer

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type RawCommand struct {
	Command   string
	Timestamp *time.Time
	Duration  *int
}

func ReadHistory() ([]RawCommand, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("cannot get home dir: %w", err)
	}

	dat, err := os.ReadFile(filepath.Join(home, ".zsh_history"))
	if err != nil {
		return nil, fmt.Errorf("cannot read file: %w", err)
	}

	return parseHistoryCommand(string(dat)), nil

}

func parseHistoryCommand(history string) []RawCommand {
	var commands []RawCommand

	lines := strings.Split(history, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		commands = append(commands, formatCommand(line))
	}

	return commands
}

func formatCommand(raw string) RawCommand {
	re := regexp.MustCompile(`^:\s*(\d+):(\d+);(.*)$`)
	m := re.FindStringSubmatch(raw)
	if m == nil {
		return RawCommand{
			Command:   raw,
			Timestamp: nil,
			Duration:  nil,
		}
	}
	parsedTs := m[1]
	parsedDur := m[2]
	command := m[3]

	tsUnix, err := strconv.ParseInt(parsedTs, 10, 64)
	if err != nil {
		return RawCommand{
			Command: raw,
		}
	}
	dur, err := strconv.Atoi(parsedDur)
	if err != nil {
		return RawCommand{
			Command: raw,
		}
	}
	t := time.Unix(tsUnix, 0)
	return RawCommand{
		Command:   command,
		Timestamp: &t,
		Duration:  &dur,
	}
}
