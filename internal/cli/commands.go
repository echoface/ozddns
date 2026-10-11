package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/echoface/ozddns/internal/app"
	"github.com/echoface/ozddns/internal/domain"
)

// ---- helpers -------------------------------------------------------------

// stringList is a repeatable string flag (e.g. --set a=b --set c=d).
type stringList []string

func (s *stringList) String() string { return strings.Join(*s, ",") }
func (s *stringList) Set(v string) error {
	*s = append(*s, v)
	return nil
}

func (e *env) table() *tabwriter.Writer {
	return tabwriter.NewWriter(e.out, 0, 2, 2, ' ', 0)
}

func subcommand(args []string, name string) (string, []string) {
	return name, args
}

func (e *env) fail(err error) int {
	fmt.Fprintln(e.err, "error:", err)
	return 1
}

// ---- status / ip ---------------------------------------------------------

// cmdHealth probes a running daemon's /healthz endpoint. It resolves the bind
// address with the same precedence the daemon used, so it works no matter how
// the address was configured (flag, OZDDNS_LISTEN, config.yaml, or default).
func (e *env) cmdHealth(args []string) int {
	fs := flag.NewFlagSet("health", flag.ContinueOnError)
	fs.SetOutput(e.err)
	listen := fs.String("listen", "", "address to probe (overrides OZDDNS_LISTEN and config.yaml)")
	timeout := fs.Duration("timeout", 3*time.Second, "how long to wait for a response")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	addr, _ := e.resolveListen(*listen)
	url := "http://" + probeHost(addr) + "/healthz"
	client := &http.Client{Timeout: *timeout}
	resp, err := client.Get(url)
	if err != nil {
		fmt.Fprintf(e.err, "unhealthy: %v\n", err)
		return 1
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(e.err, "unhealthy: %s returned HTTP %d\n", url, resp.StatusCode)
		return 1
	}
	fmt.Fprintf(e.out, "ok\t%s\n", url)
	return 0
}

// probeHost rewrites a wildcard bind address (0.0.0.0 / ::) to loopback so the
// probe connects to a concrete host.
func probeHost(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	switch host {
	case "", "0.0.0.0", "::", "[::]":
		host = "127.0.0.1"
	}
	return net.JoinHostPort(host, port)
}

func (e *env) cmdStatus(args []string) int {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	fs.SetOutput(e.err)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	return e.withApp(func(ctx context.Context, a *app.App) int {
		st, err := a.Status(ctx)
		if err != nil {
			return e.fail(err)
		}
		if e.json {
			e.printJSON(st)
			return 0
		}
		w := e.table()
		fmt.Fprintf(w, "Version\t%s (%s)\n", st.Version, st.Build)
		fmt.Fprintf(w, "Scheduler\t%s · uptime %s\n", st.Scheduler, (time.Duration(st.UptimeSeconds) * time.Second).Round(time.Second))
		fmt.Fprintf(w, "Providers\t%d\n", st.Providers)
		fmt.Fprintf(w, "Tasks\t%d (%d enabled)\n", st.Jobs, st.JobsEnabled)
		fmt.Fprintf(w, "Runs\tsuccess %d · no-change %d · failed %d · skipped %d\n",
			st.RunsSuccess, st.RunsNoChange, st.RunsFailed, st.RunsSkipped)
		fmt.Fprintf(w, "Listen\t%s (%s)\n", st.Listen, st.ListenSource)
		fmt.Fprintf(w, "Database\t%s · %s\n", st.DBDriver, st.DBPath)
		return flush(w, e)
	})
}

func (e *env) cmdIP(args []string) int {
	fs := flag.NewFlagSet("ip", flag.ContinueOnError)
	fs.SetOutput(e.err)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	return e.withApp(func(ctx context.Context, a *app.App) int {
		ip := a.CurrentIP(ctx)
		if e.json {
			e.printJSON(ip)
			return 0
		}
		fmt.Fprintf(e.out, "IPv4  %-40s (%s)\n", orDash(ip.V4), orDash(ip.V4Source))
		fmt.Fprintf(e.out, "IPv6  %-40s (%s)\n", orDash(ip.V6), orDash(ip.V6Source))
		return 0
	})
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

func flush(w *tabwriter.Writer, e *env) int {
	if err := w.Flush(); err != nil {
		return e.fail(err)
	}
	return 0
}

// ---- provider ------------------------------------------------------------

func (e *env) cmdProvider(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(e.out, "usage: ozddns provider <list|add|validate|zones|rm>")
		return 0
	}
	switch args[0] {
	case "list", "ls":
		return e.providerList(args[1:])
	case "add":
		return e.providerAdd(args[1:])
	case "validate":
		return e.providerValidate(args[1:])
	case "zones":
		return e.providerZones(args[1:])
	case "rm", "remove", "delete":
		return e.providerRemove(args[1:])
	default:
		fmt.Fprintf(e.err, "unknown provider command %q\n", args[0])
		return 2
	}
}

func (e *env) providerList(args []string) int {
	return e.withApp(func(ctx context.Context, a *app.App) int {
		views, err := a.ListProviders(ctx)
		if err != nil {
			return e.fail(err)
		}
		if e.json {
			e.printJSON(views)
			return 0
		}
		w := e.table()
		fmt.Fprintln(w, "ID\tNAME\tTYPE\tSTATUS\tENABLED\tZONES")
		for _, p := range views {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%t\t%s\n", p.ID, p.Name, p.Type, p.Status, p.Enabled, strings.Join(p.Zones, ","))
		}
		return flush(w, e)
	})
}

func (e *env) providerAdd(args []string) int {
	fs := flag.NewFlagSet("provider add", flag.ContinueOnError)
	fs.SetOutput(e.err)
	typ := fs.String("type", "", "provider type (cloudflare, alidns, dnspod, …)")
	name := fs.String("name", "", "display name")
	var sets stringList
	fs.Var(&sets, "set", "config field as key=value (repeatable)")
	disabled := fs.Bool("disabled", false, "create disabled")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *typ == "" || *name == "" {
		fmt.Fprintln(e.err, "error: --type and --name are required")
		return 2
	}
	cfg := map[string]any{}
	for _, kv := range sets {
		k, v, ok := strings.Cut(kv, "=")
		if !ok {
			fmt.Fprintf(e.err, "error: --set %q is not key=value\n", kv)
			return 2
		}
		cfg[k] = coerce(v)
	}
	return e.withApp(func(ctx context.Context, a *app.App) int {
		enabled := !*disabled
		v, err := a.CreateProvider(ctx, app.ProviderInput{Name: *name, Type: *typ, Config: cfg, Enabled: &enabled})
		if err != nil {
			return e.fail(err)
		}
		if e.json {
			e.printJSON(v)
			return 0
		}
		fmt.Fprintf(e.out, "added provider %s (%s) — status %s: %s\n", v.ID, v.Name, v.Status, v.StatusText)
		return 0
	})
}

func (e *env) providerValidate(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(e.err, "error: provider id required")
		return 2
	}
	return e.withApp(func(ctx context.Context, a *app.App) int {
		v, err := a.ValidateProvider(ctx, args[0])
		if e.json {
			e.printJSON(map[string]any{"ok": err == nil, "provider": v, "error": errString(err)})
			if err != nil {
				return 1
			}
			return 0
		}
		if err != nil {
			fmt.Fprintf(e.err, "%s: %s\n", v.Name, err)
			return 1
		}
		fmt.Fprintf(e.out, "%s: valid · zones %s\n", v.Name, strings.Join(v.Zones, ", "))
		return 0
	})
}

func (e *env) providerZones(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(e.err, "error: provider id required")
		return 2
	}
	return e.withApp(func(ctx context.Context, a *app.App) int {
		zones, err := a.Zones(ctx, args[0])
		if err != nil {
			return e.fail(err)
		}
		if e.json {
			e.printJSON(zones)
			return 0
		}
		for _, z := range zones {
			fmt.Fprintf(e.out, "%s\t%s\t%s\n", z.ID, z.Name, z.Status)
		}
		return 0
	})
}

func (e *env) providerRemove(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(e.err, "error: provider id required")
		return 2
	}
	return e.withApp(func(ctx context.Context, a *app.App) int {
		if err := a.DeleteProvider(ctx, args[0]); err != nil {
			return e.fail(err)
		}
		fmt.Fprintln(e.out, "deleted provider", args[0])
		return 0
	})
}

// ---- source --------------------------------------------------------------

func (e *env) cmdSource(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(e.out, "usage: ozddns source <list|add|test|rm>")
		return 0
	}
	switch args[0] {
	case "list", "ls":
		return e.withApp(func(ctx context.Context, a *app.App) int {
			srcs, err := a.ListIPSources(ctx)
			if err != nil {
				return e.fail(err)
			}
			if e.json {
				e.printJSON(srcs)
				return 0
			}
			w := e.table()
			fmt.Fprintln(w, "ID\tNAME\tTYPE\tFAMILY\tTARGET\tENABLED")
			for _, s := range srcs {
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%t\n", s.ID, s.Name, s.Type, s.Family, s.Target, s.Enabled)
			}
			return flush(w, e)
		})
	case "add":
		return e.sourceAdd(args[1:])
	case "test":
		return e.sourceTest(args[1:])
	case "rm", "remove", "delete":
		return e.withApp(func(ctx context.Context, a *app.App) int {
			if len(args) < 2 {
				fmt.Fprintln(e.err, "error: source id required")
				return 2
			}
			if err := a.DeleteIPSource(ctx, args[1]); err != nil {
				return e.fail(err)
			}
			fmt.Fprintln(e.out, "deleted source", args[1])
			return 0
		})
	default:
		fmt.Fprintf(e.err, "unknown source command %q\n", args[0])
		return 2
	}
}

func (e *env) sourceAdd(args []string) int {
	fs := flag.NewFlagSet("source add", flag.ContinueOnError)
	fs.SetOutput(e.err)
	name := fs.String("name", "", "display name")
	typ := fs.String("type", "http", "http | interface | command | static")
	family := fs.String("family", "v4", "v4 | v6")
	target := fs.String("target", "", "URL, NIC name, command or literal IP")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *name == "" || *target == "" {
		fmt.Fprintln(e.err, "error: --name and --target are required")
		return 2
	}
	return e.withApp(func(ctx context.Context, a *app.App) int {
		s, err := a.CreateIPSource(ctx, app.IPSourceInput{
			Name: *name, Type: domain.IPSourceKind(*typ), Family: *family, Target: *target,
		})
		if err != nil {
			return e.fail(err)
		}
		if e.json {
			e.printJSON(s)
			return 0
		}
		fmt.Fprintf(e.out, "added source %s (%s · %s)\n", s.ID, s.Name, s.Family)
		return 0
	})
}

func (e *env) sourceTest(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(e.err, "error: source id required")
		return 2
	}
	return e.withApp(func(ctx context.Context, a *app.App) int {
		ip, err := a.TestIPSource(ctx, args[0])
		if err != nil {
			return e.fail(err)
		}
		fmt.Fprintln(e.out, ip)
		return 0
	})
}

// ---- job -----------------------------------------------------------------

func (e *env) cmdJob(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(e.out, "usage: ozddns job <list|show|add|run|enable|disable|rm>")
		return 0
	}
	switch args[0] {
	case "list", "ls":
		return e.jobList(args[1:])
	case "show":
		return e.jobShow(args[1:])
	case "add":
		return e.jobAdd(args[1:])
	case "run":
		return e.jobRun(args[1:])
	case "enable", "disable":
		return e.jobToggle(args[0], args[1:])
	case "rm", "remove", "delete":
		return e.withApp(func(ctx context.Context, a *app.App) int {
			if len(args) < 2 {
				fmt.Fprintln(e.err, "error: job id required")
				return 2
			}
			if err := a.DeleteJob(ctx, args[1]); err != nil {
				return e.fail(err)
			}
			fmt.Fprintln(e.out, "deleted job", args[1])
			return 0
		})
	default:
		fmt.Fprintf(e.err, "unknown job command %q\n", args[0])
		return 2
	}
}

func (e *env) jobList(args []string) int {
	return e.withApp(func(ctx context.Context, a *app.App) int {
		jobs, err := a.ListJobs(ctx)
		if err != nil {
			return e.fail(err)
		}
		if e.json {
			e.printJSON(jobs)
			return 0
		}
		w := e.table()
		fmt.Fprintln(w, "ID\tNAME\tPROVIDER\tSOURCE\tTARGETS\tINTERVAL\tLAST\tNEXT")
		for _, j := range jobs {
			domains := make([]string, 0, len(j.Targets))
			for _, t := range j.Targets {
				domains = append(domains, string(t.Type)+":"+t.Domain)
			}
			last := string(j.State.LastStatus)
			if last == "" {
				last = "—"
			}
			next := "paused"
			if j.Enabled {
				next = relTime(j.State.NextRunAt)
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%dm\t%s\t%s\n",
				j.ID, j.Name, j.ProviderName, j.IPSourceName, strings.Join(domains, ","), j.IntervalMinutes, last, next)
		}
		return flush(w, e)
	})
}

func (e *env) jobShow(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(e.err, "error: job id required")
		return 2
	}
	return e.withApp(func(ctx context.Context, a *app.App) int {
		j, err := a.GetJob(ctx, args[0])
		if err != nil {
			return e.fail(err)
		}
		if e.json {
			e.printJSON(j)
			return 0
		}
		fmt.Fprintf(e.out, "%s (%s)\n", j.Name, j.ID)
		fmt.Fprintf(e.out, "  provider : %s [%s]\n", j.ProviderName, j.ProviderType)
		fmt.Fprintf(e.out, "  source   : %s\n", j.IPSourceName)
		fmt.Fprintf(e.out, "  interval : %dm (change-only=%t, jitter=%t, enabled=%t)\n",
			j.IntervalMinutes, j.RunOnChangeOnly, j.Jitter, j.Enabled)
		fmt.Fprintln(e.out, "  records  :")
		for _, t := range j.Targets {
			fmt.Fprintf(e.out, "    %s %s ttl=%ds proxied=%t\n", t.Type, t.Domain, t.TTL, t.Proxied)
		}
		st := j.State
		fmt.Fprintf(e.out, "  state    : %s cur=%s desired=%s next=%s\n",
			orDash(string(st.LastStatus)), orDash(st.CurrentIP), orDash(st.DesiredIP), relTime(st.NextRunAt))
		if st.LastError != "" {
			fmt.Fprintf(e.out, "  error    : %s\n", st.LastError)
		}
		return 0
	})
}

func (e *env) jobAdd(args []string) int {
	fs := flag.NewFlagSet("job add", flag.ContinueOnError)
	fs.SetOutput(e.err)
	name := fs.String("name", "", "task name")
	provider := fs.String("provider", "", "provider id or name")
	source := fs.String("source", "", "ip source id or name")
	var targets stringList
	fs.Var(&targets, "target", "domain[:type[:ttl]] (repeatable)")
	interval := fs.Int("interval", 5, "interval in minutes")
	onChange := fs.Bool("on-change", true, "write only when the address changes")
	jitter := fs.Bool("jitter", false, "add ±10% jitter to the interval")
	proxied := fs.Bool("proxied", false, "mark records proxied (Cloudflare)")
	disabled := fs.Bool("disabled", false, "create disabled")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *name == "" || *provider == "" || *source == "" || len(targets) == 0 {
		fmt.Fprintln(e.err, "error: --name, --provider, --source and at least one --target are required")
		return 2
	}
	tgt, err := parseTargets(targets, *proxied)
	if err != nil {
		fmt.Fprintln(e.err, "error:", err)
		return 2
	}
	return e.withApp(func(ctx context.Context, a *app.App) int {
		pid, err := resolveProvider(ctx, a, *provider)
		if err != nil {
			return e.fail(err)
		}
		sid, err := resolveSource(ctx, a, *source)
		if err != nil {
			return e.fail(err)
		}
		enabled := !*disabled
		j, err := a.CreateJob(ctx, app.JobInput{
			Name: *name, ProviderID: pid, IPSourceID: sid, Targets: tgt,
			IntervalMinutes: *interval, RunOnChangeOnly: onChange, Jitter: jitter, Enabled: &enabled,
		})
		if err != nil {
			return e.fail(err)
		}
		if e.json {
			e.printJSON(j)
			return 0
		}
		fmt.Fprintf(e.out, "added job %s (%s) — %d record(s)\n", j.ID, j.Name, len(j.Targets))
		return 0
	})
}

func (e *env) jobRun(args []string) int {
	fs := flag.NewFlagSet("job run", flag.ContinueOnError)
	fs.SetOutput(e.err)
	dry := fs.Bool("dry-run", false, "compute the diff without writing")
	pos, err := splitFlags(fs, args)
	if err != nil {
		return 2
	}
	if len(pos) == 0 {
		fmt.Fprintln(e.err, "error: job id required")
		return 2
	}
	id := pos[0]
	return e.withApp(func(ctx context.Context, a *app.App) int {
		if *dry {
			diff, err := a.DryRun(ctx, id)
			if err != nil {
				return e.fail(err)
			}
			if e.json {
				e.printJSON(diff)
				return 0
			}
			for _, t := range diff.Targets {
				fmt.Fprintf(e.out, "%-6s %-30s current=%-16s desired=%-16s\n",
					string(t.Action), t.Domain, orDash(t.Current), orDash(t.Desired))
			}
			return 0
		}
		run, err := a.RunNow(ctx, id)
		if e.json {
			e.printJSON(map[string]any{"run": run, "error": errString(err)})
			if err != nil && run.JobID == "" {
				return 1
			}
			return 0
		}
		if run.JobID == "" {
			return e.fail(err)
		}
		fmt.Fprintf(e.out, "%s · %s · %s in %dms\n", run.Status, diffPair(run.OldIP, run.NewIP), orDash(run.Message), run.DurationMS)
		if err != nil {
			return 1
		}
		return 0
	})
}

func (e *env) jobToggle(action string, args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(e.err, "error: job id required")
		return 2
	}
	enabled := action == "enable"
	return e.withApp(func(ctx context.Context, a *app.App) int {
		j, err := a.UpdateJob(ctx, args[0], app.JobInput{Enabled: &enabled})
		if err != nil {
			return e.fail(err)
		}
		fmt.Fprintf(e.out, "%s: %s\n", j.Name, map[bool]string{true: "enabled", false: "disabled"}[j.Enabled])
		return 0
	})
}

// ---- run -----------------------------------------------------------------

func (e *env) cmdRun(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(e.out, "usage: ozddns run <list|show>")
		return 0
	}
	switch args[0] {
	case "list", "ls":
		fs := flag.NewFlagSet("run list", flag.ContinueOnError)
		fs.SetOutput(e.err)
		job := fs.String("job", "", "filter by job id")
		status := fs.String("status", "", "filter by status")
		n := fs.Int("n", 25, "max rows")
		if err := fs.Parse(args[1:]); err != nil {
			return 2
		}
		return e.withApp(func(ctx context.Context, a *app.App) int {
			runs, err := a.ListRuns(ctx, *job, domain.RunStatus(*status), *n)
			if err != nil {
				return e.fail(err)
			}
			if e.json {
				e.printJSON(runs)
				return 0
			}
			w := e.table()
			fmt.Fprintln(w, "ID\tWHEN\tJOB\tTRIGGER\tCHANGE\tMS\tSTATUS\tMESSAGE")
			for _, r := range runs {
				fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\t%d\t%s\t%s\n",
					r.ID, r.StartedAt.Local().Format("01-02 15:04:05"), r.JobID, r.Trigger,
					diffPair(r.OldIP, r.NewIP), r.DurationMS, r.Status, r.Message)
			}
			return flush(w, e)
		})
	case "show":
		if len(args) < 2 {
			fmt.Fprintln(e.err, "error: run id required")
			return 2
		}
		id, err := strconv.ParseInt(args[1], 10, 64)
		if err != nil {
			fmt.Fprintln(e.err, "error: run id must be a number")
			return 2
		}
		return e.withApp(func(ctx context.Context, a *app.App) int {
			r, err := a.GetRun(ctx, id)
			if err != nil {
				return e.fail(err)
			}
			if e.json {
				e.printJSON(r)
				return 0
			}
			fmt.Fprintf(e.out, "run %d · %s · %s\n", r.ID, r.JobID, r.Status)
			fmt.Fprintf(e.out, "  when    : %s (%dms, %s)\n", r.StartedAt.Local().Format(time.RFC3339), r.DurationMS, r.Trigger)
			fmt.Fprintf(e.out, "  change  : %s\n", diffPair(r.OldIP, r.NewIP))
			fmt.Fprintf(e.out, "  records : %d\n", r.Records)
			fmt.Fprintf(e.out, "  message : %s\n", orDash(r.Message))
			return 0
		})
	default:
		fmt.Fprintf(e.err, "unknown run command %q\n", args[0])
		return 2
	}
}

// ---- config --------------------------------------------------------------

func (e *env) cmdConfig(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(e.out, "usage: ozddns config <export|import>")
		return 0
	}
	switch args[0] {
	case "export":
		fs := flag.NewFlagSet("config export", flag.ContinueOnError)
		fs.SetOutput(e.err)
		out := fs.String("out", "", "write to file (default stdout)")
		if err := fs.Parse(args[1:]); err != nil {
			return 2
		}
		return e.withApp(func(ctx context.Context, a *app.App) int {
			b, err := a.Export(ctx)
			if err != nil {
				return e.fail(err)
			}
			data, _ := json.MarshalIndent(b, "", "  ")
			data = append(data, '\n')
			if *out == "" {
				_, _ = e.out.Write(data)
				return 0
			}
			if err := os.WriteFile(*out, data, 0o600); err != nil {
				return e.fail(err)
			}
			fmt.Fprintf(e.out, "exported %d providers, %d sources, %d jobs → %s\n",
				len(b.Providers), len(b.IPSources), len(b.Jobs), *out)
			return 0
		})
	case "import":
		fs := flag.NewFlagSet("config import", flag.ContinueOnError)
		fs.SetOutput(e.err)
		replace := fs.Bool("replace", false, "delete existing configuration before importing")
		pos, err := splitFlags(fs, args[1:])
		if err != nil {
			return 2
		}
		if len(pos) == 0 {
			fmt.Fprintln(e.err, "error: path to a bundle JSON is required")
			return 2
		}
		data, err := os.ReadFile(pos[0])
		if err != nil {
			return e.fail(err)
		}
		var b app.ConfigBundle
		if err := json.Unmarshal(data, &b); err != nil {
			return e.fail(err)
		}
		return e.withApp(func(ctx context.Context, a *app.App) int {
			stats, err := a.Import(ctx, b, *replace)
			if err != nil {
				return e.fail(err)
			}
			fmt.Fprintf(e.out, "imported %d providers, %d sources, %d jobs\n",
				stats.Providers, stats.IPSources, stats.Jobs)
			return 0
		})
	default:
		fmt.Fprintf(e.err, "unknown config command %q\n", args[0])
		return 2
	}
}

// ---- small utilities -----------------------------------------------------

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func diffPair(old, new string) string {
	if old == "" && new == "" {
		return "—"
	}
	if old == new {
		return orDash(new)
	}
	return orDash(old) + " → " + orDash(new)
}

func relTime(t time.Time) string {
	if t.IsZero() {
		return "due now"
	}
	d := time.Until(t)
	if d <= 0 {
		return "due now"
	}
	return "in " + d.Round(time.Second).String()
}

// coerce turns "true"/"false"/numbers into their JSON types, else keeps a string.
func coerce(v string) any {
	switch strings.ToLower(v) {
	case "true", "yes", "on":
		return true
	case "false", "no", "off":
		return false
	}
	if i, err := strconv.Atoi(v); err == nil {
		return i
	}
	return v
}

func parseTargets(list []string, proxied bool) ([]domain.Target, error) {
	out := make([]domain.Target, 0, len(list))
	for _, raw := range list {
		parts := strings.Split(raw, ":")
		domain_ := strings.TrimSpace(parts[0])
		if domain_ == "" {
			return nil, fmt.Errorf("target %q has no domain", raw)
		}
		typ := domain.RecordA
		ttl := 300
		if len(parts) > 1 && strings.TrimSpace(parts[1]) != "" {
			typ = domain.RecordType(strings.ToUpper(strings.TrimSpace(parts[1])))
		}
		if len(parts) > 2 && strings.TrimSpace(parts[2]) != "" {
			n, err := strconv.Atoi(strings.TrimSpace(parts[2]))
			if err != nil {
				return nil, fmt.Errorf("target %q has an invalid TTL", raw)
			}
			ttl = n
		}
		out = append(out, domain.Target{Domain: domain_, Type: typ, TTL: ttl, Proxied: proxied})
	}
	return out, nil
}

func resolveProvider(ctx context.Context, a *app.App, idOrName string) (string, error) {
	providers, err := a.Store.Providers().List(ctx)
	if err != nil {
		return "", err
	}
	for _, p := range providers {
		if p.ID == idOrName || strings.EqualFold(p.Name, idOrName) {
			return p.ID, nil
		}
	}
	return "", fmt.Errorf("provider %q not found", idOrName)
}

func resolveSource(ctx context.Context, a *app.App, idOrName string) (string, error) {
	srcs, err := a.Store.IPSources().List(ctx)
	if err != nil {
		return "", err
	}
	for _, s := range srcs {
		if s.ID == idOrName || strings.EqualFold(s.Name, idOrName) {
			return s.ID, nil
		}
	}
	return "", fmt.Errorf("ip source %q not found", idOrName)
}

// splitFlags parses flags that may appear before or after positional arguments.
func splitFlags(fs *flag.FlagSet, args []string) ([]string, error) {
	var pos []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if strings.HasPrefix(a, "-") {
			if err := fs.Parse(args[i:]); err != nil {
				return nil, err
			}
			pos = append(pos, fs.Args()...)
			return pos, nil
		}
		pos = append(pos, a)
	}
	return pos, nil
}
