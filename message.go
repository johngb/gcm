package gcm

import "encoding/json"

// Message is used by the application server to send a message to
// the GCM server. See the documentation for GCM Architectural
// Overview for more information:
// http://developer.android.com/google/gcm/gcm.html#send-msg
type Message struct {
	// Targets
	To              string   `json:"to,omitempty"`
	RegistrationIDs []string `json:"registration_ids,omitempty"`
	Condition       string   `json:"condition,omitempty"`

	// Options

	// Data is the payload for GCMM.
	CollapseKey string `json:"collapse_key,omitempty"`

	// Valid values for Priority are "normal" and "high".
	Priority              string `json:"priority,omitempty"`
	DelayWhileIdle        bool   `json:"delay_while_idle,omitempty"`
	TimeToLive            int    `json:"time_to_live,omitempty"`
	RestrictedPackageName string `json:"restricted_package_name,omitempty"`
	DryRun                bool   `json:"dry_run,omitempty"`

	// Payload
	Data json.RawMessage `json:"data,omitempty"`
}
