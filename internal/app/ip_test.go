package app

import (
	"context"
	"strings"
	"testing"

	"github.com/echoface/ozddns/internal/domain"
)

func TestResolveIPUsesSelectedSources(t *testing.T) {
	ctx := context.Background()
	a := openApp(t)

	v4, err := a.CreateIPSource(ctx, IPSourceInput{
		Name: "selected IPv4", Type: domain.IPStatic, Family: "v4", Target: "203.0.113.40",
	})
	if err != nil {
		t.Fatal(err)
	}
	v6, err := a.CreateIPSource(ctx, IPSourceInput{
		Name: "selected IPv6", Type: domain.IPStatic, Family: "v6", Target: "2001:db8::40",
	})
	if err != nil {
		t.Fatal(err)
	}

	got := a.ResolveIP(ctx, IPSourceSelection{V4ID: v4.ID, V6ID: v6.ID})
	if got.V4.Status != IPStatusOK || got.V4.Address != "203.0.113.40" ||
		got.V4.SourceID != v4.ID || got.V4.Source != v4.Name {
		t.Fatalf("v4 result = %+v", got.V4)
	}
	if got.V6.Status != IPStatusOK || got.V6.Address != "2001:db8::40" ||
		got.V6.SourceID != v6.ID || got.V6.Source != v6.Name {
		t.Fatalf("v6 result = %+v", got.V6)
	}
	if got.CheckedAt.IsZero() || got.V4.CheckedAt == nil || got.V6.CheckedAt == nil {
		t.Fatalf("checked timestamps missing: %+v", got)
	}
}

func TestResolveIPDefaultsToFirstSourcePerFamily(t *testing.T) {
	ctx := context.Background()
	a := openApp(t)

	if _, err := a.CreateIPSource(ctx, IPSourceInput{
		Name: "first IPv4", Type: domain.IPStatic, Family: "v4", Target: "203.0.113.41",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.CreateIPSource(ctx, IPSourceInput{
		Name: "second IPv4", Type: domain.IPStatic, Family: "v4", Target: "203.0.113.42",
	}); err != nil {
		t.Fatal(err)
	}

	got := a.ResolveIP(ctx, IPSourceSelection{}).V4
	if got.Status != IPStatusOK || got.Address != "203.0.113.41" {
		t.Fatalf("default v4 result = %+v", got)
	}
	if got := a.ResolveIP(ctx, IPSourceSelection{}).V6; got.Status != IPStatusNoSource {
		t.Fatalf("default v6 result = %+v, want no source", got)
	}
}

func TestResolveIPReportsSelectedSourceFailure(t *testing.T) {
	ctx := context.Background()
	a := openApp(t)

	failing, err := a.CreateIPSource(ctx, IPSourceInput{
		Name: "broken v6 command", Type: domain.IPCommand, Family: "v6", Target: "exit 7",
	})
	if err != nil {
		t.Fatal(err)
	}

	got := a.ResolveIP(ctx, IPSourceSelection{V6ID: failing.ID}).V6
	if got.Status != IPStatusError || got.Address != "" {
		t.Fatalf("v6 result = %+v, want an explicit error", got)
	}
	if got.SourceID != failing.ID || got.Source != failing.Name {
		t.Fatalf("failure attribution = %+v", got)
	}
	if !strings.Contains(got.Error, "command failed") {
		t.Fatalf("error = %q, want command failure", got.Error)
	}
	if got.CheckedAt == nil || got.CheckedAt.IsZero() {
		t.Fatal("failed check must carry its own timestamp")
	}
}

func TestResolveIPRejectsMissingOrWrongFamilySelection(t *testing.T) {
	ctx := context.Background()
	a := openApp(t)

	v4, err := a.CreateIPSource(ctx, IPSourceInput{
		Name: "IPv4 only", Type: domain.IPStatic, Family: "v4", Target: "203.0.113.43",
	})
	if err != nil {
		t.Fatal(err)
	}

	missing := a.ResolveIP(ctx, IPSourceSelection{V4ID: "missing"}).V4
	if missing.Status != IPStatusError || !strings.Contains(missing.Error, "not found") {
		t.Fatalf("missing selection = %+v", missing)
	}

	wrongFamily := a.ResolveIP(ctx, IPSourceSelection{V6ID: v4.ID}).V6
	if wrongFamily.Status != IPStatusError || !strings.Contains(wrongFamily.Error, "not v6") {
		t.Fatalf("wrong-family selection = %+v", wrongFamily)
	}
}
