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

func ReadHistory(path string) ([]RawCommand, error) {
	if path == "" {
		path = readHistoryPath()
	}

	dat, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("cannot read file: %w", err)
	}

	return parseHistoryCommand(string(dat)), nil

}

func readHistoryPath() string {
	if hf := os.Getenv("HISTFILE"); hf != "" {
		return hf
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return ".zsh_history"
	}

	return filepath.Join(home, ".zsh_history")
}

func parseHistoryCommand(history string) []RawCommand {
	const sentinel = "\x00"
	folded := strings.ReplaceAll(history, "\\\n", sentinel)

	var commands []RawCommand
	for _, entry := range strings.Split(folded, "\n") {
		entry := strings.ReplaceAll(entry, sentinel, "\n")
		if strings.TrimSpace(entry) == "" {
			continue
		}
		commands = append(commands, formatCommand(entry))
	}
	return commands
}

func formatCommand(raw string) RawCommand {
	re := regexp.MustCompile(`(?s)^:\s*(\d+):(\d+);(.*)$`)
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
