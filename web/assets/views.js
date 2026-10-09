/* ============================================================
   OzDDNS — views (each returns an HTML string)
   ============================================================ */
(function () {
  var OZ = window.OZ, ico = window.ico, U = OZ.util, esc = U.esc, C = OZ.c;
  var V = OZ.views = {};
  var t = function (k, v) { return OZ.t(k, v); };

  function head(eyebrow, title, sub, actions) {
    return '<div class="page-head"><div class="titles"><div class="eyebrow">' + esc(eyebrow) + '</div>' +
      '<h1 class="page-title">' + esc(title) + '</h1>' +
      (sub ? '<p class="page-sub">' + sub + '</p>' : '') + '</div>' +
      (actions ? '<div class="page-actions">' + actions + '</div>' : '') + '</div>';
  }
  function jobRecordCell(j) {
    var tg = j.targets[0];
    return '<div class="hstack" style="gap:8px">' + C.rtype(tg.type) +
      '<div><div class="row-title mono" style="font-size:13px">' + esc(tg.domain) + '</div>' +
      '<div class="row-sub">' + (j.targets.length > 1 ? t('{n} more', { n: j.targets.length - 1 }) + ' · ' : '') + esc(j.current || '—') + '</div></div></div>';
  }
  function jobRow(j) {
    return '<tr data-act="go" data-href="#/jobs/' + j.id + '">' +
      '<td><div class="row-title">' + esc(j.name) + '</div><div class="row-sub">' + esc(OZ.get.providerType(OZ.get.provider(j.providerId).type).label) + '</div></td>' +
      '<td>' + jobRecordCell(j) + '</td>' +
      '<td>' + C.runPill(j.lastStatus) + '</td>' +
      '<td class="mono tiny">' + esc(U.fmtAgo(j.lastRun)) + '</td>' +
      '<td class="mono tiny">' + esc(j.enabled ? U.fmtIn(j.nextRun) : t('paused')) + '</td>' +
      '<td class="r"><button class="btn sm" data-act="run-job" data-id="' + j.id + '">' + ico('play') + 'Run</button></td>' +
      '</tr>';
  }

  /* ---------------- Overview ---------------- */
  V.overview = function () {
    var D = OZ.data, jobs = D.jobs;
    var failing = jobs.filter(function (j) { return j.lastStatus === 'failed'; });
    var enabled = jobs.filter(function (j) { return j.enabled; });
    var records = D.providers.reduce(function (a, p) { return a + p.records; }, 0);
    var failRuns = D.runs.filter(function (r) { return r.status === 'failed' && (Date.now() - r.startedAt) < 86400000; }).length;
    var changes = D.runs.filter(function (r) { return r.status === 'success' && (Date.now() - r.startedAt) < 86400000; }).length;

    var stats = '<div class="grid cols-4 mb24 reveal">' +
      C.stat('Public IPv4', D.ipInfo.v4, esc(t('via {src} · {ago}', { src: D.ipInfo.v4Source, ago: U.fmtAgo(D.ipInfo.v4Checked) })), 'globe', C.spark('slate', [3, 4, 3, 5, 4, 5, 4, 6])) +
      C.stat('Active tasks', enabled.length + '<small> / ' + jobs.length + '</small>', esc(t('{n} writes in 24h', { n: changes })), 'jobs', C.spark('olive', [2, 3, 3, 4, 3, 4, 5, 4])) +
      C.stat('Records managed', records, esc(t('across {n} providers', { n: D.providers.length })), 'database') +
      C.stat('Failures · 24h', failRuns, failRuns ? 'needs attention' : 'all clear', 'alert-triangle', C.spark('rust', [1, 2, 1, 3, 2, 5, 3, 4])) +
      '</div>';

    var iphero = '<div class="card mb24"><div class="ip-hero">' +
      '<div class="col"><div class="hstack between"><span class="lab stat" style="padding:0"><span class="eyebrow">Public IPv4</span></span>' +
      '<span class="pill mute"><span class="dot"></span>' + esc(U.fmtAgo(D.ipInfo.v4Checked)) + '</span></div>' +
      '<div class="ipv4 mt8">' + esc(D.ipInfo.v4) + '</div>' +
      '<div class="row-sub mt8">' + esc(t('Source · {s} · used by {n} tasks', { s: D.ipInfo.v4Source, n: jobs.length })) + '</div>' +
      '<div class="hstack mt16"><button class="btn sm" data-act="refresh-ip">' + ico('refresh') + 'Refresh IP</button>' +
      '<span class="tiny muted">Re-checks the public address</span></div></div>' +
      '<div class="col"><div class="hstack between"><span class="eyebrow">Public IPv6</span>' +
      '<span class="pill mute"><span class="dot"></span>' + esc(U.fmtAgo(D.ipInfo.v6Checked)) + '</span></div>' +
      '<div class="ipv6 mt8">' + esc(D.ipInfo.v6) + '</div>' +
      '<div class="row-sub mt8">Source · ' + esc(D.ipInfo.v6Source) + '</div>' +
      '<div class="callout mt16">' + t('Some hosts rotate their IPv6 prefix daily. Enable {b} writes to keep records tidy.', { b: '<b>' + esc(t('on change')) + '</b>' }) + '</div></div>' +
      '</div></div>';

    var warn = failing.length ? C.banner('err', t(failing.length > 1 ? '{n} tasks need attention' : '{n} task needs attention', { n: failing.length }),
      '<b>' + esc(failing[0].name) + '</b> — ' + esc(failing[0].error || 'update failed') + '. <a href="#/jobs/' + failing[0].id + '" style="color:var(--rust);text-decoration:underline">Open task</a>') : '';

    var tasksCard = '<div class="card"><div class="card-head"><span class="h">Sync tasks</span><span class="spacer"></span>' +
      '<a class="btn sm ghost" href="#/jobs">View all ' + ico('chevron-right') + '</a></div>' +
      '<table class="table clickable"><thead><tr><th>Task</th><th>Record</th><th>Last result</th><th>Ran</th><th>Next</th><th></th></tr></thead>' +
      '<tbody>' + jobs.slice(0, 6).map(jobRow).join('') + '</tbody></table></div>';

    var actCard = '<div class="card"><div class="card-head"><span class="h">Activity</span><span class="spacer"></span>' +
      '<button class="icon-btn" data-act="go" data-href="#/executions">' + ico('activity') + '</button></div><div class="card-body">' +
      '<div class="timeline">' + D.activity.slice(0, 6).map(function (a, i, arr) {
        return '<div class="tl-item"><div class="tl-rail"><span class="tl-node ' + a.kind + '"></span>' + (i < arr.length - 1 ? '<span class="tl-line"></span>' : '') + '</div>' +
          '<div class="tl-body"><div class="tl-text"><b>' + esc(a.title) + '</b></div>' +
          '<div class="tl-text muted">' + esc(a.text) + '</div>' +
          '<div class="tl-time">' + esc(U.fmtAgo(a.at)) + '</div></div></div>';
      }).join('') + '</div></div></div>';

    return head('Control room', 'Overview',
      'Everything OzDDNS is keeping in sync right now — providers, tasks, and the reconciliation between your hosts and DNS.',
      '<button class="btn primary" data-act="go" data-href="#/jobs/new">' + ico('plus') + 'New task</button>') +
      stats + iphero + (warn ? '<div class="mb24">' + warn + '</div>' : '') +
      '<div class="grid" style="grid-template-columns:1.7fr 1fr">' + tasksCard + actCard + '</div>';
  };

  /* ---------------- Providers list ---------------- */
  V.providers = function () {
    var D = OZ.data, f = OZ.state.providerFilter;
    var list = D.providers.filter(function (p) {
      if (f === 'all') return true;
      if (f === 'ok') return p.status === 'ok' && p.enabled;
      if (f === 'warn') return p.status === 'warn';
      if (f === 'err') return p.status === 'err';
      return true;
    });
    var actions = '<button class="btn primary" data-act="open-modal" data-modal="new-provider">' + ico('plus') + 'Add provider</button>';

    var cards = list.map(function (p) {
      var pt = OZ.get.providerType(p.type);
      return '<div class="card pad" data-act="go" data-href="#/providers/' + p.id + '" style="cursor:pointer;display:flex;flex-direction:column;gap:14px">' +
        '<div class="hstack"><span class="pv" style="flex:1"><span class="logo lg" style="color:var(--pv-' + pt.color + ');background:var(--pv-' + pt.color + '-bg)">' +
        (pt.label.match(/[A-Za-z]/g) || []).slice(0, 2).join('').toUpperCase() + '</span>' +
        '<span><span class="pv-name" style="font-size:15px">' + esc(p.name) + '</span><br><span class="pv-type">' + esc(pt.label) + '</span></span></span>' +
        C.providerStatus(p) + '</div>' +
        '<div class="hstack " style="gap:6px;flex-wrap:wrap">' + p.zones.map(function (z) { return '<span class="pill mute">' + esc(z) + '</span>'; }).join('') + '</div>' +
        '<div class="hstack between" style="border-top:1px solid var(--line-2);padding-top:12px">' +
        '<span class="tiny muted">' + esc(p.statusText) + '</span>' +
        '<span class="hstack tiny muted" style="gap:6px">' + ico('hash') + esc(t('{n} records', { n: p.records })) + '</span></div>' +
        '</div>';
    }).join('');

    return head('Providers', 'DNS providers',
      'Accounts and credentials OzDDNS may write to. A provider is validated once and reused by any number of tasks.',
      actions) +
      '<div class="between mb16"><div class="seg">' +
      [['all', 'All', D.providers.length], ['ok', 'Connected', D.providers.filter(function (p) { return p.status === 'ok' && p.enabled; }).length],
       ['warn', 'Attention', D.providers.filter(function (p) { return p.status === 'warn'; }).length],
       ['err', 'Errors', D.providers.filter(function (p) { return p.status === 'err'; }).length]].map(function (s) {
        return '<button class="' + (f === s[0] ? 'on' : '') + '" data-act="set-filter" data-key="providerFilter" data-value="' + s[0] + '">' + esc(t(s[1])) + ' · ' + s[2] + '</button>';
      }).join('') + '</div>' +
      '<span class="tiny muted">' + esc(t('Filter by state, or use the search box above to jump anywhere.')) + '</span></div>' +
      (list.length ? '<div class="grid cols-3 reveal">' + cards + '</div>'
        : '<div class="card">' + C.empty('server', 'No providers here',
          'Nothing matches this filter. Add a provider to start writing DNS records.', '<button class="btn primary" data-act="open-modal" data-modal="new-provider">' + ico('plus') + 'Add provider</button>') + '</div>');
  };

  /* ---------------- Provider detail ---------------- */
  V.provider = function (id) {
    var p = OZ.get.provider(id);
    if (!p) return notFound('Provider not found');
    var pt = OZ.get.providerType(p.type);
    var tab = OZ.state.param && OZ.state.param.indexOf('/') > -1 ? OZ.state.param.split('/')[1] : 'overview';
    var usingJobs = OZ.data.jobs.filter(function (j) { return j.providerId === p.id; });

    var tabs = '<div class="tabs mb16">' +
      ['overview', 'configuration', 'usage'].map(function (k) {
        return '<button class="' + (tab === k ? 'on' : '') + '" data-act="go" data-href="#/providers/' + p.id + (k === 'overview' ? '' : '/' + k) + '">' +
          ({ overview: t('Overview'), configuration: t('Configuration'), usage: t('Used by ({n})', { n: usingJobs.length }) })[k] + '</button>';
      }).join('') + '</div>';

    var body;
    if (tab === 'configuration') {
      body = '<div class="grid cols-2"><div class="card pad">' +
        (pt.fields || []).map(function (f) { return C.field({ field: f, key: p.id + '_' + f.key, value: p.config[f.key] != null ? p.config[f.key] : '' }); }).join('') +
        '<div class="hstack mt8"><button class="btn primary" data-act="save-provider" data-id="' + p.id + '">' + ico('save') + 'Save changes</button>' +
        '<button class="btn" data-act="validate-provider" data-id="' + p.id + '">' + ico('shield-check') + 'Test connection</button></div>' +
        '</div><div class="vstack"><div class="card pad">' + C.banner('info', 'Secrets are stored encrypted',
          'Values are masked in the UI and never written to logs. API responses redact credentials by default.') + '</div>' +
        '<div class="card pad"><div class="strong mb8">Provider capability</div>' +
        '<div class="switch-row"><div class="txt"><div class="t">Proxied records</div><div class="d">Toggle orange-cloud proxying per record</div></div>' + C.pill(pt.supportsProxied ? 'ok' : 'mute', pt.supportsProxied ? 'Supported' : 'N/A') + '</div>' +
        '<div class="switch-row"><div class="txt"><div class="t">IPv6 (AAAA)</div><div class="d">Write AAAA records</div></div>' + C.pill(pt.tag === 'godaddy' ? 'warn' : 'ok', pt.tag === 'godaddy' ? 'Limited' : 'Supported') + '</div></div></div></div>';
    } else if (tab === 'usage') {
      body = usingJobs.length ? '<div class="card"><table class="table clickable"><thead><tr><th>Task</th><th>Record</th><th>Interval</th><th>Status</th></tr></thead><tbody>' +
        usingJobs.map(function (j) { return '<tr data-act="go" data-href="#/jobs/' + j.id + '"><td class="row-title">' + esc(j.name) + '</td><td>' + jobRecordCell(j) + '</td><td class="mono tiny">' + esc(t('{n} min', { n: j.intervalMinutes })) + '</td><td>' + C.runPill(j.lastStatus) + '</td></tr>'; }).join('') +
        '</tbody></table></div>' : '<div class="card">' + C.empty('jobs', 'Not used yet', 'No task writes through this provider. Create a task to start syncing.') + '</div>';
    } else {
      body = '<div class="grid cols-2"><div class="card"><div class="card-head"><span class="h">Zones</span></div>' +
        '<table class="table"><thead><tr><th>Zone</th><th>Records</th><th>Visibility</th></tr></thead><tbody>' +
        (p.zones.length ? p.zones.map(function (z) { return '<tr><td class="mono">' + esc(z) + '</td><td class="num">' + Math.ceil(p.records / p.zones.length) + '</td><td>' + C.pill('ok', 'Read / Write') + '</td></tr>'; }).join('') : '<tr><td colspan="3" class="muted">No zones discovered.</td></tr>') +
        '</tbody></table></div>' +
        '<div class="card pad"><div class="strong mb12">Details</div>' +
        kv('Provider type', pt.label) + kv('Created', U.fmtDateTime(p.createdAt)) + kv('Last validated', U.fmtAgo(p.lastValidated)) +
        kv('Status', C.providerStatus(p)) + kv('Records tracked', String(p.records)) +
        '<div class="divider"></div><button class="btn danger block" data-act="delete-provider" data-id="' + p.id + '">' + ico('trash') + 'Delete provider</button></div></div>';
    }

    return '<div class="crumb mb16"><a href="#/providers" class="muted">Providers</a><span class="sep">/</span><b>' + esc(p.name) + '</b></div>' +
      '<div class="page-head"><div class="hstack" style="gap:16px;flex:1">' + C.providerLogo(p.type, true) +
      '<div><h1 class="page-title">' + esc(p.name) + '</h1>' +
      '<div class="hstack mt8" style="gap:8px">' + C.providerStatus(p) + '<span class="tiny muted">' + esc(pt.label) + ' · ' + esc(p.statusText) + '</span></div></div></div>' +
      '<div class="page-actions"><button class="btn" data-act="validate-provider" data-id="' + p.id + '">' + ico('shield-check') + 'Validate</button>' +
      '<button class="btn primary" data-act="go" data-href="#/providers/' + p.id + '/configuration">' + ico('pencil') + 'Edit</button></div></div>' +
      (p.status === 'err' ? '<div class="mb16">' + C.banner('err', 'This provider is failing', esc(p.statusText) + ' ' + esc(t('Re-enter credentials or re-issue a token, then validate again.'))) + '</div>' : '') +
      (p.status === 'warn' ? '<div class="mb16">' + C.banner('warn', 'Heads up', esc(p.statusText) + ' ' + esc(t('Rotate the credential before it expires to avoid interrupted updates.'))) + '</div>' : '') +
      tabs + body;
  };
  function kv(k, v) { return '<div class="between" style="padding:7px 0;border-bottom:1px solid var(--line-2)"><span class="tiny muted">' + esc(k) + '</span><span class="strong" style="font-size:13px">' + v + '</span></div>'; }

  /* ---------------- Jobs list ---------------- */
  V.jobs = function () {
    var D = OZ.data;
    var rows = D.jobs.map(function (j) {
      return '<tr data-act="go" data-href="#/jobs/' + j.id + '">' +
        '<td><div class="hstack" style="gap:10px">' + C.toggle(j.enabled, 'toggle-job', j.id) +
        '<div><div class="row-title">' + esc(j.name) + '</div><div class="row-sub">' + esc(OZ.get.providerType(OZ.get.provider(j.providerId).type).label) + '</div></div></div></td>' +
        '<td>' + jobRecordCell(j) + '</td>' +
        '<td class="mono tiny">' + esc(t('{n} min', { n: j.intervalMinutes })) + (j.runOnChangeOnly ? ' <span class="pill mute" style="font-size:10px">' + esc(t('on change')) + '</span>' : '') + '</td>' +
        '<td>' + C.runPill(j.lastStatus) + '</td>' +
        '<td class="mono tiny">' + esc(j.enabled ? U.fmtIn(j.nextRun) : '—') + '</td>' +
        '<td class="r"><div class="hstack" style="justify-content:flex-end;gap:6px">' +
        '<button class="btn sm" data-act="run-job" data-id="' + j.id + '">' + ico('play') + 'Run</button>' +
        '<button class="icon-btn" data-act="go" data-href="#/jobs/' + j.id + '">' + ico('chevron-right') + '</button></div></td></tr>';
    }).join('');

    return head('Sync', 'Sync tasks',
      'A task pairs an IP source with a provider and a set of records. On each tick OzDDNS diffs desired vs. actual and writes only what drifted.',
      '<button class="btn" data-act="run-all" title="Reconcile every enabled task now — the schedule is unchanged">' + ico('play') + 'Run all</button><button class="btn primary" data-act="go" data-href="#/jobs/new">' + ico('plus') + 'New task</button>') +
      (D.jobs.some(function (j) { return j.lastStatus === 'failed'; })
        ? '<div class="mb16">' + C.banner('err', 'A task is failing', 'One or more tasks could not write. Open the failing task to see the provider response.') + '</div>' : '') +
      '<div class="card"><table class="table clickable"><thead><tr><th style="width:26%">Task</th><th>Record</th><th>Interval</th><th>Last result</th><th>Next run</th><th></th></tr></thead><tbody>' +
      rows + '</tbody></table></div>';
  };

  function notFound(t) { return '<div class="card">' + C.empty('alert-triangle', t || 'Not found', 'The thing you were looking for does not exist (or was deleted).', '<a class="btn primary" href="#/overview">Back to overview</a>') + '</div>'; }
  OZ.views.notFound = notFound;
})();
