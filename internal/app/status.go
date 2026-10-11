package app

import (
	"context"
	"runtime"
	"time"

	"github.com/echoface/ozddns/internal/domain"
	"github.com/echoface/ozddns/internal/version"
)

// RuntimeStatus is the panel's "system" summary.
type RuntimeStatus struct {
	Version        string    `json:"version"`
	Build          string    `json:"build"`
	Scheduler      string    `json:"scheduler"` // running | paused
	UptimeSeconds  int64     `json:"uptimeSeconds"`
	Providers      int       `json:"providers"`
	Jobs           int       `json:"jobs"`
	JobsEnabled    int       `json:"jobsEnabled"`
	RunsSuccess    int       `json:"runsSuccess"`
	RunsNoChange   int       `json:"runsNoChange"`
	RunsFailed     int       `json:"runsFailed"`
	RunsSkipped    int       `json:"runsSkipped"`
	DBDriver       string    `json:"dbDriver"`
	DBPath         string    `json:"dbPath"`
	Listen         string    `json:"listen"`
	ListenSource   string    `json:"listenSource"`
	BasePath       string    `json:"basePath"`
	BasePathSource string    `json:"basePathSource"`
	GoVersion      string    `json:"goVersion"`
	GOOS           string    `json:"goos"`
	GOARCH         string    `json:"goarch"`
	ServerTime     time.Time `json:"serverTime"`
}

// Status assembles the runtime summary.
func (a *App) Status(ctx context.Context) (RuntimeStatus, error) {
	providers, _ := a.Store.Providers().List(ctx)
	jobs, _ := a.Store.Jobs().List(ctx)
	enabled := 0
	for _, j := range jobs {
		if j.Enabled {
			enabled++
		}
	}
	st := RuntimeStatus{
		Version: version.Version, Build: version.Build,
		Scheduler:     boolStr(a.Running(), "running", "paused"),
		UptimeSeconds: int64(a.Uptime().Seconds()),
		Providers:     len(providers), Jobs: len(jobs), JobsEnabled: enabled,
		DBDriver: a.DBDriver, DBPath: a.DBPath,
		Listen: a.ListenAddr, ListenSource: a.ListenSource,
		BasePath: basePathOrRoot(a.BasePath), BasePathSource: a.BasePathSource,
		GoVersion: runtime.Version(), GOOS: runtime.GOOS, GOARCH: runtime.GOARCH,
		ServerTime: time.Now().UTC(),
	}
	st.RunsSuccess, _ = a.Store.Runs().CountByStatus(ctx, domain.StatusSuccess)
	st.RunsNoChange, _ = a.Store.Runs().CountByStatus(ctx, domain.StatusNoChange)
	st.RunsFailed, _ = a.Store.Runs().CountByStatus(ctx, domain.StatusFailed)
	st.RunsSkipped, _ = a.Store.Runs().CountByStatus(ctx, domain.StatusSkipped)
	return st, nil
}

// basePathOrRoot keeps the status payload meaningful for applications built
// outside the CLI (tests, embedders) that never set a base path.
func basePathOrRoot(v string) string {
	if v == "" {
		return "/"
	}
	return v
}

func boolStr(v bool, t, f string) string {
	if v {
		return t
	}
	return f
}
