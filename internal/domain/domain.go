// Package domain holds the core entities shared by the engine, storage,
// application services, REST API and CLI. It has no dependencies on any
// infrastructure package.
package domain

import (
	"encoding/json"
	"time"
)

// ---------------------------------------------------------------------------
// Records & changes (the vocabulary of a DNS provider)
// ---------------------------------------------------------------------------

// RecordType is a DNS record type. OzDDNS only manages the small set needed by
// dynamic DNS.
type RecordType string

const (
	RecordA     RecordType = "A"
	RecordAAAA  RecordType = "AAAA"
	RecordCNAME RecordType = "CNAME"
)

// Family returns "v4" or "v6" for a record type.
func (t RecordType) Family() string {
	if t == RecordAAAA {
		return "v6"
	}
	return "v4"
}

// Record is a single DNS record as returned by a provider.
type Record struct {
	ID      string     `json:"id"`
	Name    string     `json:"name"`
	Type    RecordType `json:"type"`
	Content string     `json:"content"`
	TTL     int        `json:"ttl"`
	Proxied bool       `json:"proxied,omitempty"`
	Comment string     `json:"comment,omitempty"`
}

// RecordFilter narrows a ListRecords call.
type RecordFilter struct {
	Name string
	Type RecordType
}

// Zone is a DNS zone (root domain) belonging to a provider account.
type Zone struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status,omitempty"`
}

// ChangeKind describes what Apply should do.
type ChangeKind string

const (
	ChangeCreate ChangeKind = "create"
	ChangeUpdate ChangeKind = "update"
	ChangeDelete ChangeKind = "delete"
)

// Change is a single create/update/delete request handed to a provider. The
// record carries the full name; adapters resolve the zone themselves.
type Change struct {
	Kind   ChangeKind `json:"kind"`
	Record Record     `json:"record"`
}

// ---------------------------------------------------------------------------
// Provider instances
// ---------------------------------------------------------------------------

// Provider is a configured DNS account/credential (a "provider instance").
type Provider struct {
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	Type      string         `json:"type"`
	Config    map[string]any `json:"config"`
	Enabled   bool           `json:"enabled"`
	CreatedAt time.Time      `json:"createdAt"`
	UpdatedAt time.Time      `json:"updatedAt"`
}

// ---------------------------------------------------------------------------
// IP sources
// ---------------------------------------------------------------------------

// IPSourceKind enumerates how an address is obtained.
type IPSourceKind string

const (
	IPHTTP      IPSourceKind = "http"
	IPInterface IPSourceKind = "interface"
	IPCommand   IPSourceKind = "command"
	IPStatic    IPSourceKind = "static"
)

// IPSource describes where an address comes from.
type IPSource struct {
	ID        string       `json:"id"`
	Name      string       `json:"name"`
	Type      IPSourceKind `json:"type"`
	Family    string       `json:"family"` // v4 | v6
	Target    string       `json:"target"`
	Enabled   bool         `json:"enabled"`
	CreatedAt time.Time    `json:"createdAt"`
	UpdatedAt time.Time    `json:"updatedAt"`
}

// ---------------------------------------------------------------------------
// Sync jobs
// ---------------------------------------------------------------------------

// Target is one DNS record a job keeps in sync.
type Target struct {
	Domain  string     `json:"domain"`
	Type    RecordType `json:"type"`
	TTL     int        `json:"ttl"`
	Proxied bool       `json:"proxied"`
	Comment string     `json:"comment,omitempty"`
}

// Job is a sync task: provider + IP source + records + schedule.
type Job struct {
	ID              string    `json:"id"`
	Name            string    `json:"name"`
	ProviderID      string    `json:"providerId"`
	IPSourceID      string    `json:"ipSourceId"`
	Targets         []Target  `json:"targets"`
	IntervalMinutes int       `json:"intervalMinutes"`
	RunOnChangeOnly bool      `json:"runOnChangeOnly"`
	Jitter          bool      `json:"jitter"`
	Enabled         bool      `json:"enabled"`
	CreatedAt       time.Time `json:"createdAt"`
	UpdatedAt       time.Time `json:"updatedAt"`
}

// ---------------------------------------------------------------------------
// Runs (execution history) and per-job runtime state
// ---------------------------------------------------------------------------

// RunStatus is the outcome of one reconciliation.
type RunStatus string

const (
	StatusSuccess  RunStatus = "success"  // at least one record written
	StatusNoChange RunStatus = "nochange" // everything already matched
	StatusFailed   RunStatus = "failed"   // provider/network error
	StatusSkipped  RunStatus = "skipped"  // no address available, nothing to do
)

// Trigger is what caused a run.
type Trigger string

const (
	TriggerSchedule Trigger = "schedule"
	TriggerManual   Trigger = "manual"
)

// Run is one reconciliation attempt.
type Run struct {
	ID         int64     `json:"id"`
	JobID      string    `json:"jobId"`
	StartedAt  time.Time `json:"startedAt"`
	FinishedAt time.Time `json:"finishedAt"`
	DurationMS int       `json:"durationMs"`
	Status     RunStatus `json:"status"`
	OldIP      string    `json:"oldIp,omitempty"`
	NewIP      string    `json:"newIp,omitempty"`
	Trigger    Trigger   `json:"trigger"`
	Message    string    `json:"message,omitempty"`
	Records    int       `json:"records,omitempty"`
}

// JobState is the mutable runtime state of a job (kept out of the job config).
type JobState struct {
	JobID         string    `json:"jobId"`
	LastStatus    RunStatus `json:"lastStatus"`
	LastRunAt     time.Time `json:"lastRunAt"`
	NextRunAt     time.Time `json:"nextRunAt"`
	CurrentIP     string    `json:"current"`
	DesiredIP     string    `json:"desired"`
	LastError     string    `json:"error,omitempty"`
	LastChangedAt time.Time `json:"lastChangedAt"`
}

// ---------------------------------------------------------------------------
// Settings & notifications
// ---------------------------------------------------------------------------

// Notification is a delivery channel.
type Notification struct {
	ID      string   `json:"id"`
	Kind    string   `json:"kind"` // slack | discord | ntfy | gotify | webhook
	Name    string   `json:"name"`
	Target  string   `json:"target"`
	Enabled bool     `json:"enabled"`
	On      []string `json:"on"` // change | failure | success
}

// Settings holds instance-level configuration.
type Settings struct {
	Language        string `json:"language"`
	Theme           string `json:"theme"`
	Timezone        string `json:"timezone"`
	DefaultInterval int    `json:"defaultInterval"`
	// Scheduler tuning. Zero values mean "use the built-in default", so an
	// older settings row without these keys keeps behaving as before.
	SchedulerPaused      bool `json:"schedulerPaused"`      // true = automatic runs off
	SchedulerTickSeconds int  `json:"schedulerTickSeconds"` // how often due jobs are checked
	SchedulerWorkers     int  `json:"schedulerWorkers"`     // max concurrent reconciliations
	RunOnStart           bool `json:"runOnStart"`           // reconcile once when the daemon boots
	// How many days of execution history to keep. The daemon purges anything
	// older, once an hour, with a real DELETE — the log is a rolling window,
	// not an archive, so the database cannot grow without bound. Zero means
	// "use the built-in default" (see app.DefaultRunRetentionDays).
	RunRetentionDays int            `json:"runRetentionDays"`
	Username         string         `json:"username"`
	PasswordHash     string         `json:"-"`
	PasswordSet      bool           `json:"passwordSet"`
	AllowWAN         bool           `json:"allowWan"`
	TLS              bool           `json:"tls"`
	Listen           string         `json:"listen"`
	Notifications    []Notification `json:"notifications"`
}

// MarshalConfig is a helper for providers that need the raw JSON of a config.
func MarshalConfig(m map[string]any) json.RawMessage {
	b, _ := json.Marshal(m)
	return b
}
