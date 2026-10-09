// Package engine owns scheduling and reconciliation. Providers only read and
// apply; deciding *what* to write lives here so every provider behaves the same
// and the UI can show an honest diff.
package engine

import (
	"context"
	"fmt"
	"net/netip"
	"strings"

	"github.com/echoface/ozddns/internal/domain"
	"github.com/echoface/ozddns/internal/ipsource"
	"github.com/echoface/ozddns/internal/provider"
)

// Result summarises one reconciliation.
type Result struct {
	Status  domain.RunStatus
	OldIP   string
	NewIP   string
	Message string
	Records int // number of records created or updated
}

// Reconciler diffs desired DNS state against actual and writes only what drifted.
type Reconciler struct {
	Providers *provider.Registry
	Sources   *ipsource.Registry
}

// NewReconciler builds a reconciler.
func NewReconciler(p *provider.Registry, s *ipsource.Registry) *Reconciler {
	return &Reconciler{Providers: p, Sources: s}
}

// Reconcile brings a job's targets in line with the address its IP source reports.
func (r *Reconciler) Reconcile(ctx context.Context, job domain.Job, prov domain.Provider, src domain.IPSource) (Result, error) {
	adapter, err := r.Providers.Build(prov)
	if err != nil {
		return Result{Status: domain.StatusFailed}, err
	}

	// Resolve one address per family that the targets require.
	need := map[string]bool{}
	for _, t := range job.Targets {
		need[t.Type.Family()] = true
	}
	addrs := map[string]netip.Addr{}
	for fam := range need {
		// Ask the source for the family the target needs, regardless of the
		// source's configured default (an HTTP endpoint can serve either).
		s := src
		s.Family = fam
		if addr, err := r.Sources.Resolve(ctx, s); err == nil {
			addrs[fam] = addr
		}
	}

	res := Result{Status: domain.StatusNoChange}
	var changed int
	var skipped int
	var messages []string

	for _, t := range job.Targets {
		fam := t.Type.Family()
		addr, ok := addrs[fam]
		if !ok {
			skipped++
			messages = append(messages, fmt.Sprintf("no %s address for %s", fam, t.Domain))
			continue
		}
		desired := addr.String()
		if res.NewIP == "" {
			res.NewIP = desired
		}

		existing, err := adapter.ListRecords(ctx, t.Domain, domain.RecordFilter{Name: t.Domain, Type: t.Type})
		if err != nil {
			res.Status = domain.StatusFailed
			res.Message = err.Error()
			return res, err
		}
		if len(existing) > 0 && res.OldIP == "" {
			res.OldIP = existing[0].Content
		}

		ttl := t.TTL
		rec := domain.Record{Name: t.Domain, Type: t.Type, Content: desired, TTL: ttl, Proxied: t.Proxied, Comment: t.Comment}

		if len(existing) == 0 {
			if err := adapter.Apply(ctx, domain.Change{Kind: domain.ChangeCreate, Record: rec}); err != nil {
				res.Status = domain.StatusFailed
				res.Message = err.Error()
				return res, err
			}
			changed++
			continue
		}

		// Reconcile against the first matching record (dynamic DNS is 1:1).
		cur := existing[0]
		ipChanged := !sameAddr(cur.Content, desired)
		proxiedChanged := prov.Type == "cloudflare" && cur.Proxied != t.Proxied
		if !ipChanged && !proxiedChanged {
			continue
		}
		upd := rec
		upd.ID = cur.ID
		if err := adapter.Apply(ctx, domain.Change{Kind: domain.ChangeUpdate, Record: upd}); err != nil {
			res.Status = domain.StatusFailed
			res.Message = err.Error()
			return res, err
		}
		changed++
	}

	res.Records = changed
	switch {
	case changed > 0:
		res.Status = domain.StatusSuccess
		res.Message = fmt.Sprintf("updated %d record(s)", changed)
	case skipped > 0 && skipped == len(job.Targets):
		res.Status = domain.StatusSkipped
		res.Message = strings.Join(messages, "; ")
	default:
		res.Status = domain.StatusNoChange
		res.Message = "records already match the desired address"
	}
	return res, nil
}

func sameAddr(a, b string) bool {
	ap, err1 := netip.ParseAddr(strings.TrimSpace(a))
	bp, err2 := netip.ParseAddr(strings.TrimSpace(b))
	if err1 == nil && err2 == nil {
		return ap == bp
	}
	return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b))
}

// TargetDiff describes the planned action for one target.
type TargetDiff struct {
	Domain  string            `json:"domain"`
	Type    domain.RecordType `json:"type"`
	Current string            `json:"current"`
	Desired string            `json:"desired"`
	Action  ChangeAction      `json:"action"` // create | update | none | skip
}

// ChangeAction enumerates planned actions in a dry run.
type ChangeAction string

const (
	ActionCreate ChangeAction = "create"
	ActionUpdate ChangeAction = "update"
	ActionNone   ChangeAction = "none"
	ActionSkip   ChangeAction = "skip"
)

// Diff is the result of a dry run: what the reconciler would do.
type Diff struct {
	Targets []TargetDiff `json:"targets"`
}

// DryRun computes the diff without writing anything.
func (r *Reconciler) DryRun(ctx context.Context, job domain.Job, prov domain.Provider, src domain.IPSource) (Diff, error) {
	adapter, err := r.Providers.Build(prov)
	if err != nil {
		return Diff{}, err
	}
	need := map[string]bool{}
	for _, t := range job.Targets {
		need[t.Type.Family()] = true
	}
	addrs := map[string]netip.Addr{}
	for fam := range need {
		s := src
		s.Family = fam
		if addr, err := r.Sources.Resolve(ctx, s); err == nil {
			addrs[fam] = addr
		}
	}
	var diff Diff
	for _, t := range job.Targets {
		td := TargetDiff{Domain: t.Domain, Type: t.Type}
		addr, ok := addrs[t.Type.Family()]
		if !ok {
			td.Action = ActionSkip
			diff.Targets = append(diff.Targets, td)
			continue
		}
		td.Desired = addr.String()
		existing, err := adapter.ListRecords(ctx, t.Domain, domain.RecordFilter{Name: t.Domain, Type: t.Type})
		if err != nil {
			return Diff{}, err
		}
		if len(existing) == 0 {
			td.Action = ActionCreate
		} else {
			td.Current = existing[0].Content
			proxiedChanged := prov.Type == "cloudflare" && existing[0].Proxied != t.Proxied
			if sameAddr(td.Current, td.Desired) && !proxiedChanged {
				td.Action = ActionNone
			} else {
				td.Action = ActionUpdate
			}
		}
		diff.Targets = append(diff.Targets, td)
	}
	return diff, nil
}
