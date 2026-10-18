package app

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/echoface/ozddns/internal/domain"
)

func TestExportImportRoundTrip(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	src, closeSrc, err := Open(ctx, Options{DBPath: filepath.Join(dir, "a.db")})
	if err != nil {
		t.Fatalf("open src: %v", err)
	}
	defer closeSrc()

	enabled := true
	p, err := src.CreateProvider(ctx, ProviderInput{Name: "Hook", Type: "custom",
		Config: map[string]any{"url": "http://127.0.0.1:9/hook"}, Enabled: &enabled})
	if err != nil {
		t.Fatalf("CreateProvider: %v", err)
	}
	s, err := src.CreateIPSource(ctx, IPSourceInput{Name: "Fixed", Type: domain.IPStatic, Family: "v4", Target: "203.0.113.9"})
	if err != nil {
		t.Fatalf("CreateIPSource: %v", err)
	}
	if _, err := src.CreateJob(ctx, JobInput{Name: "Home", ProviderID: p.ID, IPSourceID: s.ID,
		Targets: []domain.Target{{Domain: "home.example.com", Type: domain.RecordA, TTL: 300}}, IntervalMinutes: 5}); err != nil {
		t.Fatalf("CreateJob: %v", err)
	}

	bundle, err := src.Export(ctx)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	if len(bundle.Providers) != 1 || len(bundle.IPSources) != 1 || len(bundle.Jobs) != 1 {
		t.Fatalf("bundle = %+v", bundle)
	}
	dst, closeDst, err := Open(ctx, Options{DBPath: filepath.Join(dir, "b.db")})
	if err != nil {
		t.Fatalf("open dst: %v", err)
	}
	defer closeDst()
	stats, err := dst.Import(ctx, bundle, false)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if stats.Providers != 1 || stats.IPSources != 1 || stats.Jobs != 1 {
		t.Fatalf("stats = %+v", stats)
	}
	jobs, _ := dst.ListJobs(ctx)
	if len(jobs) != 1 || jobs[0].ProviderName != "Hook" || len(jobs[0].Targets) != 1 {
		t.Fatalf("imported jobs = %+v", jobs)
	}
}

func TestImportReplace(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	a, closeA, err := Open(ctx, Options{DBPath: filepath.Join(dir, "a.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer closeA()
	if _, err := a.CreateProvider(ctx, ProviderInput{Name: "One", Type: "custom", Config: map[string]any{"url": "http://x"}}); err != nil {
		t.Fatal(err)
	}
	bundle, _ := a.Export(ctx)

	// A second provider added after the export must be gone after a replace import.
	if _, err := a.CreateProvider(ctx, ProviderInput{Name: "Two", Type: "custom", Config: map[string]any{"url": "http://y"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Import(ctx, bundle, true); err != nil {
		t.Fatalf("Import replace: %v", err)
	}
	providers, _ := a.ListProviders(ctx)
	if len(providers) != 1 || providers[0].Name != "One" {
		t.Fatalf("providers after replace = %+v", providers)
	}
}

func TestImportReplaceAppliesSchedulerSettings(t *testing.T) {
	ctx := context.Background()
	a := openApp(t)
	workers, tick, paused := 7, 11, true
	settings, err := a.PatchSettings(ctx, SettingsInput{
		SchedulerWorkers: &workers, SchedulerTickSeconds: &tick, SchedulerPaused: &paused,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Import(ctx, ConfigBundle{Settings: settings}, true); err != nil {
		t.Fatalf("import: %v", err)
	}
	gotTick, gotWorkers := a.Scheduler.Config()
	if gotTick != 11*time.Second || gotWorkers != 7 {
		t.Fatalf("scheduler = tick %s workers %d, want 11s and 7", gotTick, gotWorkers)
	}
	if a.Running() {
		t.Fatal("imported paused setting was not applied")
	}
}

// A bundle that fails validation is rejected before anything is deleted or
// written, so a malformed backup cannot wipe the running configuration.
func TestImportRejectsBadBundleWithoutMutating(t *testing.T) {
	ctx := context.Background()
	a := openApp(t)
	if _, err := a.CreateProvider(ctx, ProviderInput{Name: "One", Type: "custom", Config: map[string]any{"url": "http://x"}}); err != nil {
		t.Fatal(err)
	}

	bad := ConfigBundle{
		Providers: []domain.Provider{{ID: "p1", Name: "bad", Type: "does-not-exist"}},
		Jobs: []domain.Job{{ID: "j1", Name: "j", ProviderID: "p1", IPSourceID: "s1",
			Targets: []domain.Target{{Domain: "a.example.com", Type: domain.RecordA}}}},
	}
	if _, err := a.Import(ctx, bad, true); err == nil {
		t.Fatal("import of an unknown provider type should fail")
	}
	providers, _ := a.ListProviders(ctx)
	if len(providers) != 1 || providers[0].Name != "One" {
		t.Fatalf("existing configuration was mutated: %+v", providers)
	}
}

// A bundle whose job points at a resource it does not contain is rejected.
func TestImportRejectsDanglingReference(t *testing.T) {
	ctx := context.Background()
	a := openApp(t)
	bad := ConfigBundle{
		Providers: []domain.Provider{{ID: "p1", Name: "x", Type: "custom", Config: map[string]any{"url": "http://x"}}},
		Jobs: []domain.Job{{ID: "j1", Name: "j", ProviderID: "p1", IPSourceID: "missing",
			Targets: []domain.Target{{Domain: "a.example.com", Type: domain.RecordA}}}},
	}
	if _, err := a.Import(ctx, bad, true); err == nil {
		t.Fatal("import with a dangling ip source should fail")
	}
}

func TestImportRejectsTargetSourceFamilyMismatch(t *testing.T) {
	ctx := context.Background()
	a := openApp(t)
	bad := ConfigBundle{
		Providers: []domain.Provider{{ID: "p1", Name: "x", Type: "custom", Config: map[string]any{"url": "http://x"}}},
		IPSources: []domain.IPSource{{ID: "s1", Name: "v4", Type: domain.IPStatic, Family: "v4", Target: "203.0.113.9"}},
		Jobs: []domain.Job{{ID: "j1", Name: "j", ProviderID: "p1", IPSourceID: "s1",
			Targets: []domain.Target{{Domain: "v6.example.com", Type: domain.RecordAAAA}}}},
	}
	if _, err := a.Import(ctx, bad, true); err == nil {
		t.Fatal("import must reject an AAAA target backed by a v4 source")
	}
}
