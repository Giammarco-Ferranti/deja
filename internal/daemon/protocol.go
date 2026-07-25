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

// SetConfigReq mutates daemon-side settings. An omitted field means "leave
// alone": "" for Fuzzy, nil for Empty. Empty is a *bool (not bool) so a
// pointer-to-false — "turn suggestions off" — is distinguishable from absent
// (omitempty only drops nil, never a non-nil pointer to false).
type SetConfigReq struct {
	Fuzzy string `json:"fuzzy,omitempty"`
	Empty *bool  `json:"empty,omitempty"`
}

// SetConfigResp echoes the resulting effective settings. Error is non-empty
// when the request was rejected (invalid value); the previous setting is kept.
// Empty is a pointer for the same reason it is in GetConfigResp.
type SetConfigResp struct {
	Fuzzy string `json:"fuzzy"`
	Empty *bool  `json:"empty,omitempty"`
	Error string `json:"error,omitempty"`
}

type GetConfigReq struct{}

// GetConfigResp reports the daemon's current settings. Empty is a *bool so a
// client talking to an older daemon that predates this field decodes nil (and
// can fall back to its own config default) rather than a misleading false.
type GetConfigResp struct {
	Fuzzy string `json:"fuzzy"`
	Empty *bool  `json:"empty,omitempty"`
}
