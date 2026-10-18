// Package engine owns scheduling and reconciliation. Providers only read and
// apply; deciding *what* to write lives here so every provider behaves the same
// and the UI can show an honest diff.
package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/netip"
	"sort"
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
	Records int                // number of records created or updated
	Details []domain.RunDetail // per-record outcome, in target order
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

// Plan resolves the address a job needs without touching the DNS provider. A
// job is bound to one address family: every target must agree with the source,
// so a misconfigured API/CLI payload cannot write an IPv4 value into AAAA (or
// vice versa). Resolution failures are returned rather than hidden behind a
// generic "no address" skip.
func (r *Reconciler) Plan(ctx context.Context, job domain.Job, src domain.IPSource) (map[string]netip.Addr, error) {
	if src.Family != "v4" && src.Family != "v6" {
		return nil, fmt.Errorf("ip source %q has invalid family %q", src.Name, src.Family)
	}
	need := false
	for _, t := range job.Targets {
		if !t.Type.IsAddressRecord() {
			continue
		}
		if family := t.Type.Family(); family != src.Family {
			return nil, fmt.Errorf("target %s requires %s but ip source %q provides %s",
				t.Domain, family, src.Name, src.Family)
		}
		need = true
	}
	addrs := map[string]netip.Addr{}
	if !need {
		return addrs, nil
	}
	addr, err := r.Sources.Resolve(ctx, src)
	if err != nil {
		return nil, fmt.Errorf("resolve IP source %q: %w", src.Name, err)
	}
	addr = addr.Unmap()
	if (src.Family == "v6" && !addr.Is6()) || (src.Family == "v4" && !addr.Is4()) {
		return nil, fmt.Errorf("ip source %q returned %s for %s", src.Name, addr, src.Family)
	}
	addrs[src.Family] = addr
	return addrs, nil
}

// Fingerprint is a stable, canonical digest of everything a run depends on: the
// provider (including its configuration), the IP source definition, the target
// set, and the addresses the source resolved. It lets a task recognise "nothing
// that matters has changed since the last successful run", so run-on-change-only
// (and write-only providers, which cannot be read back) can skip the provider
// call. It deliberately includes more than the address, so editing a task,
// re-pointing its source, or rotating a credential on the provider always forces
// a fresh run even while the address is unchanged.
//
// Provider configuration is folded in as a hash, never in the clear: the
// fingerprint is persisted in job state and returned by the API, and provider
// configs hold credentials.
func Fingerprint(job domain.Job, prov domain.Provider, src domain.IPSource, addrs map[string]netip.Addr) string {
	fams := make([]string, 0, len(addrs))
	for fam := range addrs {
		fams = append(fams, fam)
	}
	sort.Strings(fams)
	var b strings.Builder
	b.WriteString("prov=")
	b.WriteString(job.ProviderID)
	b.WriteString(";provcfg=")
	b.WriteString(configDigest(prov.Config))
	b.WriteString(";src=")
	b.WriteString(job.IPSourceID)
	b.WriteString(";srcdef=")
	b.WriteString(string(src.Type))
	b.WriteByte('|')
	b.WriteString(src.Family)
	b.WriteByte('|')
	b.WriteString(src.Target)
	b.WriteString(";addr=")
	for i, fam := range fams {
		if i > 0 {
			b.WriteByte(';')
		}
		b.WriteString(fam)
		b.WriteByte('=')
		b.WriteString(addrs[fam].String())
	}
	b.WriteString(";targets=")
	for i, t := range job.Targets {
		if i > 0 {
			b.WriteByte(';')
		}
		fmt.Fprintf(&b, "%s|%s|%d|%t", t.Domain, t.Type, t.TTL, t.Proxied)
	}
	return b.String()
}

// configDigest hashes a provider config map into a stable, non-reversible
// string. Key order is normalised so an unrelated map reordering cannot look
// like a change, and values are never included verbatim.
func configDigest(cfg map[string]any) string {
	if len(cfg) == 0 {
		return ""
	}
	raw, err := json.Marshal(cfg)
	if err != nil {
		// Provider configs are decoded from JSON in normal use. Keep the
		// fallback deterministic for unusual embedders without losing the
		// no-plaintext property of the persisted fingerprint.
		raw = []byte(fmt.Sprintf("%v", cfg))
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// writeOnly reports whether a provider cannot read records back, in which case
// the reconciler must not try to diff against a read: it just writes.
func (r *Reconciler) writeOnly(prov domain.Provider) bool {
	f, ok := r.Providers.Get(prov.Type)
	return ok && f.Meta().WriteOnly
}

// Reconcile brings a job's targets in line with the address its IP source reports.
func (r *Reconciler) Reconcile(ctx context.Context, job domain.Job, prov domain.Provider, src domain.IPSource) (Result, error) {
	addrs, err := r.Plan(ctx, job, src)
	if err != nil {
		return Result{Status: domain.StatusFailed, Message: err.Error()}, err
	}
	return r.ReconcileWith(ctx, job, prov, addrs)
}

// ReconcileWith is Reconcile against addresses the caller already resolved via
// Plan, so a run-on-change-only task resolves the source only once.
func (r *Reconciler) ReconcileWith(ctx context.Context, job domain.Job, prov domain.Provider, addrs map[string]netip.Addr) (Result, error) {
	adapter, err := r.Providers.Build(prov)
	if err != nil {
		return Result{Status: domain.StatusFailed}, err
	}

	res := Result{Status: domain.StatusNoChange}
	var changed int
	var skipped int
	var messages []string
	// A write-only provider has no read API, so there is nothing to diff
	// against: the desired address is pushed as-is and reported as "applied".
	writeOnly := r.writeOnly(prov)

	for _, t := range job.Targets {
		// A CNAME target has no address to reconcile: its value is a hostname,
		// which an IP source cannot supply. New jobs are rejected at validation;
		// a legacy stored one is reported rather than silently written wrong.
		if !t.Type.IsAddressRecord() {
			skipped++
			note := fmt.Sprintf("%s record %s is not address-driven; skipped", t.Type, t.Domain)
			messages = append(messages, note)
			res.Details = append(res.Details, domain.RunDetail{
				Domain: t.Domain, Type: t.Type, Action: domain.RunActionSkipped, Note: note,
			})
			continue
		}
		fam := t.Type.Family()
		addr, ok := addrs[fam]
		if !ok {
			skipped++
			note := fmt.Sprintf("no %s address for %s", fam, t.Domain)
			messages = append(messages, note)
			res.Details = append(res.Details, domain.RunDetail{
				Domain: t.Domain, Type: t.Type, Action: domain.RunActionSkipped, Note: note,
			})
			continue
		}
		desired := addr.String()
		if res.NewIP == "" {
			res.NewIP = desired
		}
		ttl := t.TTL
		rec := domain.Record{Name: t.Domain, Type: t.Type, Content: desired, TTL: ttl, Proxied: t.Proxied, Comment: t.Comment}

		if writeOnly {
			if res.OldIP == "" {
				res.OldIP = desired
			}
			if err := adapter.Apply(ctx, domain.Change{Kind: domain.ChangeUpdate, Record: rec}); err != nil {
				res.Status = domain.StatusFailed
				res.Message = err.Error()
				res.Details = append(res.Details, domain.RunDetail{
					Domain: t.Domain, Type: t.Type, Action: domain.RunActionFailed, Desired: desired, Proxied: t.Proxied, Note: err.Error(),
				})
				return res, err
			}
			changed++
			res.Details = append(res.Details, domain.RunDetail{
				Domain: t.Domain, Type: t.Type, Action: domain.RunActionApplied, Desired: desired, Proxied: t.Proxied,
				Note: "write-only provider: address pushed, no read API to diff against",
			})
			continue
		}

		existing, err := adapter.ListRecords(ctx, t.Domain, domain.RecordFilter{Name: t.Domain, Type: t.Type})
		if err != nil {
			res.Status = domain.StatusFailed
			res.Message = err.Error()
			res.Details = append(res.Details, domain.RunDetail{
				Domain: t.Domain, Type: t.Type, Action: domain.RunActionFailed, Desired: desired, Note: err.Error(),
			})
			return res, err
		}

		var real domain.Record
		for _, item := range existing {
			if item.ID != "" {
				real = item
				break
			}
		}
		if real.ID == "" {
			if res.OldIP == "" {
				res.OldIP = desired
			}
			if err := adapter.Apply(ctx, domain.Change{Kind: domain.ChangeCreate, Record: rec}); err != nil {
				res.Status = domain.StatusFailed
				res.Message = err.Error()
				res.Details = append(res.Details, domain.RunDetail{
					Domain: t.Domain, Type: t.Type, Action: domain.RunActionFailed, Desired: desired, Proxied: t.Proxied, Note: err.Error(),
				})
				return res, err
			}
			changed++
			res.Details = append(res.Details, domain.RunDetail{
				Domain: t.Domain, Type: t.Type, Action: domain.RunActionCreated, Desired: desired, Proxied: t.Proxied,
			})
			continue
		}

		// Reconcile against the first persistent record (dynamic DNS is 1:1);
		// providers may return an ID-less lookup placeholder as well.
		cur := real
		if res.OldIP == "" {
			res.OldIP = cur.Content
		}
		ipChanged := !sameAddr(cur.Content, desired)
		proxiedChanged := prov.Type == "cloudflare" && cur.Proxied != t.Proxied
		// Only compare TTL when the target asks for one and the provider reported
		// one; a zero on either side means "unknown / provider default", which
		// must not be treated as drift (it would rewrite forever).
		ttlChanged := t.TTL > 0 && cur.TTL > 0 && cur.TTL != t.TTL
		if !ipChanged && !proxiedChanged && !ttlChanged {
			res.Details = append(res.Details, domain.RunDetail{
				Domain: t.Domain, Type: t.Type, Action: domain.RunActionUnchanged,
				Current: cur.Content, Desired: desired, Proxied: t.Proxied,
			})
			continue
		}
		upd := rec
		upd.ID = cur.ID
		if err := adapter.Apply(ctx, domain.Change{Kind: domain.ChangeUpdate, Record: upd}); err != nil {
			res.Status = domain.StatusFailed
			res.Message = err.Error()
			res.Details = append(res.Details, domain.RunDetail{
				Domain: t.Domain, Type: t.Type, Action: domain.RunActionFailed,
				Current: cur.Content, Desired: desired, Proxied: t.Proxied, Note: err.Error(),
			})
			return res, err
		}
		changed++
		res.Details = append(res.Details, domain.RunDetail{
			Domain: t.Domain, Type: t.Type, Action: domain.RunActionUpdated,
			Current: cur.Content, Desired: desired, Proxied: t.Proxied,
		})
	}

	res.Records = changed
	switch {
	case changed > 0:
		res.Status = domain.StatusSuccess
		if writeOnly {
			res.Message = fmt.Sprintf("address pushed to the write-only provider (%d target(s))", changed)
		} else {
			res.Message = fmt.Sprintf("updated %d record(s)", changed)
		}
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
	Domain     string            `json:"domain"`
	Type       domain.RecordType `json:"type"`
	Current    string            `json:"current"`
	Desired    string            `json:"desired"`
	TTL        int               `json:"ttl,omitempty"`        // desired TTL
	CurrentTTL int               `json:"currentTtl,omitempty"` // as reported by the provider
	Action     ChangeAction      `json:"action"`               // create | update | none | skip
}

// ChangeAction enumerates planned actions in a dry run.
type ChangeAction string

const (
	ActionCreate ChangeAction = "create"
	ActionUpdate ChangeAction = "update"
	// ActionApply is a planned write to a provider that cannot be read back:
	// the value will be pushed, but there is no current value to diff against.
	ActionApply ChangeAction = "apply"
	ActionNone  ChangeAction = "none"
	ActionSkip  ChangeAction = "skip"
)

// Diff is the result of a dry run: what the reconciler would do.
type Diff struct {
	Targets []TargetDiff `json:"targets"`
}

// DryRun computes the diff without writing anything.
func (r *Reconciler) DryRun(ctx context.Context, job domain.Job, prov domain.Provider, src domain.IPSource) (Diff, error) {
	addrs, err := r.Plan(ctx, job, src)
	if err != nil {
		return Diff{}, err
	}
	return r.DryRunWith(ctx, job, prov, addrs)
}

// DryRunWith is DryRun against addresses the caller already resolved via Plan.
func (r *Reconciler) DryRunWith(ctx context.Context, job domain.Job, prov domain.Provider, addrs map[string]netip.Addr) (Diff, error) {
	adapter, err := r.Providers.Build(prov)
	if err != nil {
		return Diff{}, err
	}
	writeOnly := r.writeOnly(prov)
	var diff Diff
	for _, t := range job.Targets {
		td := TargetDiff{Domain: t.Domain, Type: t.Type, TTL: t.TTL}
		if !t.Type.IsAddressRecord() {
			td.Action = ActionSkip
			diff.Targets = append(diff.Targets, td)
			continue
		}
		addr, ok := addrs[t.Type.Family()]
		if !ok {
			td.Action = ActionSkip
			diff.Targets = append(diff.Targets, td)
			continue
		}
		td.Desired = addr.String()
		if writeOnly {
			td.Action = ActionApply
			diff.Targets = append(diff.Targets, td)
			continue
		}
		existing, err := adapter.ListRecords(ctx, t.Domain, domain.RecordFilter{Name: t.Domain, Type: t.Type})
		if err != nil {
			return Diff{}, err
		}
		var real domain.Record
		for _, item := range existing {
			if item.ID != "" {
				real = item
				break
			}
		}
		if real.ID == "" {
			td.Action = ActionCreate
		} else {
			td.Current = real.Content
			td.CurrentTTL = real.TTL
			proxiedChanged := prov.Type == "cloudflare" && real.Proxied != t.Proxied
			ttlChanged := t.TTL > 0 && real.TTL > 0 && real.TTL != t.TTL
			if sameAddr(td.Current, td.Desired) && !proxiedChanged && !ttlChanged {
				td.Action = ActionNone
			} else {
				td.Action = ActionUpdate
			}
		}
		diff.Targets = append(diff.Targets, td)
	}
	return diff, nil
}
