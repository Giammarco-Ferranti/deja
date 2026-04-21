package daemon

import "encoding/json"

// Envelope is the outer shape of every socket message. The payload is
// interpreted based on Type — one of: "suggest", "record", "ping".
type Envelope struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

type SuggestReq struct {
	Buffer string `json:"buffer"`
	Dir    string `json:"dir"`
	Prev   string `json:"prev"`
}

type SuggestResp struct {
	Suggestion   string   `json:"suggestion"`
	Alternatives []string `json:"alternatives,omitempty"`
}

type RecordReq struct {
	Command    string `json:"command"`
	Dir        string `json:"dir"`
	ExitCode   int    `json:"exit_code"`
	DurationMS int    `json:"duration_ms"`
	SessionID  string `json:"session_id"`
	Prev       string `json:"prev"`
}

type RecordResp struct{}

type PingResp struct {
	Pong bool `json:"pong"`
}
