package daemon

import "encoding/json"

// Envelope is the outer shape of every socket message. The payload is
// interpreted based on Type — one of: "suggest", "record", "ping",
// "setconfig", "getconfig".
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

// SetConfigReq mutates daemon-side settings. Empty fields mean "leave alone".
type SetConfigReq struct {
	Fuzzy string `json:"fuzzy,omitempty"`
}

// SetConfigResp echoes the resulting effective settings. Error is non-empty
// when the request was rejected (invalid value); the previous setting is kept.
type SetConfigResp struct {
	Fuzzy string `json:"fuzzy"`
	Error string `json:"error,omitempty"`
}

type GetConfigReq struct{}

type GetConfigResp struct {
	Fuzzy string `json:"fuzzy"`
}
