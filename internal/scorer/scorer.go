package scorer

import (
	"fmt"
	"time"

	"github.com/sahilm/fuzzy"
)

type Command struct {
	Id          string
	Command     string
	Directory   string
	Timestamp   time.Time
	Exit_code   string
	Duration_ms string
	Session_id  string
}

var commands = []Command{
	{Id: "1", Command: "git status", Directory: "/home/user/project", Timestamp: time.Now(), Exit_code: "0", Duration_ms: "42", Session_id: "sess-001"},
	{Id: "2", Command: "git add .", Directory: "/home/user/project", Timestamp: time.Now(), Exit_code: "0", Duration_ms: "15", Session_id: "sess-001"},
	{Id: "3", Command: "git commit -m 'fix: update readme'", Directory: "/home/user/project", Timestamp: time.Now(), Exit_code: "0", Duration_ms: "200", Session_id: "sess-001"},
	{Id: "4", Command: "go test ./...", Directory: "/home/user/project", Timestamp: time.Now(), Exit_code: "0", Duration_ms: "1200", Session_id: "sess-002"},
	{Id: "5", Command: "go build ./cmd/deja", Directory: "/home/user/project", Timestamp: time.Now(), Exit_code: "0", Duration_ms: "800", Session_id: "sess-002"},
	{Id: "6", Command: "ls -la", Directory: "/home/user", Timestamp: time.Now(), Exit_code: "0", Duration_ms: "5", Session_id: "sess-003"},
	{Id: "7", Command: "cd project", Directory: "/home/user", Timestamp: time.Now(), Exit_code: "0", Duration_ms: "2", Session_id: "sess-003"},
	{Id: "8", Command: "make build", Directory: "/home/user/project", Timestamp: time.Now(), Exit_code: "0", Duration_ms: "950", Session_id: "sess-003"},
}

// dir affinity needs to check wether the current directory match the directory
func dirAffinity(currentDir string, dir string) {

}

func Scorer(pattern string) {
	// final = 1.0 × fuzzy
	//     + 0.4 × frecency
	//     + 0.3 × directory_affinity
	//     + 0.5 × sequence_score
	var commandsList []string
	for _, c := range commands {
		commandsList = append(commandsList, c.Command)
	}
	matches := fuzzy.Find(pattern, commandsList)
	fmt.Println("matches:")
	for i, m := range matches {
		fmt.Printf("%d. %s\n", i+1, m.Str)
	}
}
