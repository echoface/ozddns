package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/echoface/ozddns/internal/domain"
	"github.com/echoface/ozddns/internal/ids"
)

// IPSourceUsers lists the names of the tasks that reference an IP source. It
// backs both the "used by" guard on delete and the family lock on update.
func (a *App) IPSourceUsers(ctx context.Context, id string) ([]string, error) {
	a.configMu.RLock()
	defer a.configMu.RUnlock()
	return a.ipSourceUsers(ctx, id)
}

func (a *App) ipSourceUsers(ctx context.Context, id string) ([]string, error) {
	jobs, err := a.Store.Jobs().List(ctx)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, j := range jobs {
		if j.IPSourceID == id {
			names = append(names, j.Name)
		}
	}
	return names, nil
}

// IPSourceInput is the payload for creating/updating an IP source.
type IPSourceInput struct {
	Name   string              `json:"name"`
	Type   domain.IPSourceKind `json:"type"`
	Family string              `json:"family"`
	Target string              `json:"target"`
}

// ListIPSources returns all IP sources.
func (a *App) ListIPSources(ctx context.Context) ([]domain.IPSource, error) {
	a.configMu.RLock()
	defer a.configMu.RUnlock()
	return a.Store.IPSources().List(ctx)
}

// CreateIPSource validates and stores an IP source.
func (a *App) CreateIPSource(ctx context.Context, in IPSourceInput) (domain.IPSource, error) {
	a.configMu.RLock()
	defer a.configMu.RUnlock()

	if err := a.validateIPSource(in); err != nil {
		return domain.IPSource{}, err
	}
	s := domain.IPSource{ID: ids.New("src"), Name: in.Name, Type: in.Type, Family: in.Family, Target: in.Target}
	if err := a.Store.IPSources().Create(ctx, s); err != nil {
		return domain.IPSource{}, err
	}
	return a.Store.IPSources().Get(ctx, s.ID)
}

// UpdateIPSource updates an IP source.
func (a *App) UpdateIPSource(ctx context.Context, id string, in IPSourceInput) (domain.IPSource, error) {
	a.configMu.RLock()
	defer a.configMu.RUnlock()

	s, err := a.Store.IPSources().Get(ctx, id)
	if err != nil {
		return domain.IPSource{}, err
	}
	// The address family is not a free edit: every task that reuses this source
	// derives its record type (A / AAAA) from it, so flipping it under a running
	// task would silently contradict the records that task writes.
	if in.Family != "" && in.Family != s.Family {
		if users, uerr := a.ipSourceUsers(ctx, id); uerr == nil && len(users) > 0 {
			return domain.IPSource{}, fmt.Errorf(
				"address family is locked: %s still use this source — reassign those tasks first, or add a new source",
				strings.Join(users, ", "))
		}
	}
	if in.Name != "" {
		s.Name = in.Name
	}
	if in.Type != "" {
		s.Type = in.Type
	}
	if in.Family != "" {
		s.Family = in.Family
	}
	if in.Target != "" {
		s.Target = in.Target
	}
	if err := a.validateIPSource(IPSourceInput{Name: s.Name, Type: s.Type, Family: s.Family, Target: s.Target}); err != nil {
		return domain.IPSource{}, err
	}
	if err := a.Store.IPSources().Update(ctx, s); err != nil {
		return domain.IPSource{}, err
	}
	return s, nil
}

// DeleteIPSource removes an IP source unless a job uses it.
func (a *App) DeleteIPSource(ctx context.Context, id string) error {
	a.configMu.RLock()
	defer a.configMu.RUnlock()

	if users, _ := a.ipSourceUsers(ctx, id); len(users) > 0 {
		return fmt.Errorf("IP source is used by task %q; reassign it first", users[0])
	}
	return a.Store.IPSources().Delete(ctx, id)
}

// TestIPSource resolves an address right now (used by the panel's "Test" button).
func (a *App) TestIPSource(ctx context.Context, id string) (string, error) {
	a.configMu.RLock()
	defer a.configMu.RUnlock()

	s, err := a.Store.IPSources().Get(ctx, id)
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	addr, err := a.Sources.Resolve(ctx, s)
	if err != nil {
		return "", err
	}
	return addr.String(), nil
}

func (a *App) validateIPSource(in IPSourceInput) error {
	if in.Name == "" {
		return fmt.Errorf("ip source name is required")
	}
	switch in.Type {
	case domain.IPHTTP, domain.IPInterface, domain.IPCommand, domain.IPStatic:
	default:
		return fmt.Errorf("unknown ip source type %q", in.Type)
	}
	if in.Family != "v4" && in.Family != "v6" {
		return fmt.Errorf("family must be v4 or v6")
	}
	if in.Target == "" {
		return fmt.Errorf("target is required")
	}
	if _, err := a.Sources.Build(domain.IPSource{Type: in.Type, Family: in.Family, Target: in.Target}); err != nil {
		return err
	}
	return nil
}
