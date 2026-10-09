/* OzDDNS — task detail view */
(function () {
  var OZ = window.OZ, ico = window.ico, U = OZ.util, esc = U.esc, C = OZ.c;
  var t = function (k, v) { return OZ.t(k, v); };
  function kv(k, v) { return '<div class="between" style="padding:7px 0;border-bottom:1px solid var(--line-2)"><span class="tiny muted">' + esc(k) + '</span><span class="strong" style="font-size:13px">' + v + '</span></div>'; }

  OZ.views.job = function (id) {
    var j = OZ.get.job(id);
    if (!j) return OZ.views.notFound('Task not found');
    var prov = OZ.get.provider(j.providerId), src = OZ.get.ipSource(j.ipSourceId);
    var runs = OZ.get.runsFor(j.id).sort(function (a, b) { return b.startedAt - a.startedAt; }).slice(0, 8);

    var targets = '<div class="card mb16"><div class="card-head"><span class="h">Reconciliation</span><span class="spacer"></span>' +
      C.pill(j.lastStatus === 'failed' ? 'err' : 'ok', j.lastStatus === 'failed' ? 'Out of sync' : 'In sync', true) + '</div>' +
      '<table class="table"><thead><tr><th>Record</th><th>Current</th><th>Desired</th><th>TTL</th><th>Flags</th></tr></thead><tbody>' +
      j.targets.map(function (t) {
        var flag = t.proxied ? C.pill('coral', 'Proxied') : '';
        return '<tr><td><div class="hstack" style="gap:8px">' + C.rtype(t.type) + '<span class="mono" style="font-size:13px">' + esc(t.domain) + '</span></div>' +
          (t.comment ? '<div class="row-sub">' + esc(t.comment) + '</div>' : '') + '</td>' +
          '<td class="mono tiny">' + esc(j.current) + '</td>' +
          '<td>' + C.diff(j.current, j.desired) + '</td>' +
          '<td class="mono tiny">' + t.ttl + 's</td><td>' + (flag || '<span class="muted tiny">DNS only</span>') + '</td></tr>';
      }).join('') + '</tbody></table>' +
      '<div class="card-body" style="border-top:1px solid var(--line-2)">' +
      '<span class="tiny muted">' + esc(t('Last reconciled {ago} · next {next}', { ago: U.fmtAgo(j.lastRun), next: j.enabled ? U.fmtIn(j.nextRun) : t('paused') })) + '</span>' +
      '</div></div>';

    // The configuration card is purely informational: "Edit" and "Delete" live
    // once, in the page header, so the same action never appears twice.
    var details = '<div class="card pad mb16"><div class="strong mb12">Configuration</div>' +
      kv('Provider', esc(OZ.get.providerType(prov.type).label)) +
      kv('IP source', esc(src.name) + ' <span class="pill mute" style="font-size:10px">' + esc(src.family.toUpperCase()) + '</span>') +
      kv('Interval', esc(t('{n} min', { n: j.intervalMinutes }))) +
      kv('Write mode', j.runOnChangeOnly ? 'Only on change' : 'Always rewrite') +
      kv('Jitter', j.jitter ? 'Enabled (±10%)' : 'Off') +
      kv('Last changed', j.lastChanged ? esc(U.fmtAgo(j.lastChanged)) : 'never') + '</div>';

    var history = '<div class="card"><div class="card-head"><span class="h">Run history</span><span class="spacer"></span>' +
      '<a class="btn sm ghost" href="#/executions">All executions' + ico('chevron-right') + '</a></div>' +
      '<table class="table clickable"><thead><tr><th>When</th><th>Trigger</th><th>Change</th><th>Duration</th><th>Result</th></tr></thead><tbody>' +
      runs.map(function (r) {
        return '<tr data-act="open-modal" data-modal="run-detail" data-id="' + r.id + '">' +
          '<td class="mono tiny">' + esc(U.fmtDateTime(r.startedAt)) + '</td>' +
          '<td>' + C.pill(r.trigger === 'manual' ? 'info' : 'mute', t(r.trigger === 'manual' ? 'manual' : 'schedule')) + '</td>' +
          '<td>' + C.diff(r.oldIp, r.newIp) + '</td>' +
          '<td class="mono tiny">' + esc(U.fmtDur(r.durationMs)) + '</td>' +
          '<td>' + C.runPill(r.status) + '</td></tr>';
      }).join('') + '</tbody></table></div>';

    return '<div class="crumb mb16"><a href="#/jobs" class="muted">Sync tasks</a><span class="sep">/</span><b>' + esc(j.name) + '</b></div>' +
      '<div class="page-head"><div class="titles"><div class="hstack" style="gap:12px">' + C.toggle(j.enabled, 'toggle-job', j.id) +
      '<h1 class="page-title" style="font-size:26px">' + esc(j.name) + '</h1>' + C.runPill(j.lastStatus) + '</div>' +
      '<p class="page-sub">' + esc(t('{src} → {provider} · {n} record(s)', { src: (src ? src.name : '—'), provider: OZ.get.providerType(prov.type).label, n: j.targets.length })) + '</p></div>' +
      '<div class="page-actions"><button class="btn" data-act="run-job" data-id="' + j.id + '">' + ico('play') + 'Run now</button>' +
      '<button class="btn primary" data-act="go" data-href="#/jobs/' + j.id + '/edit">' + ico('pencil') + t('Edit') + '</button>' +
      '<button class="btn danger" data-act="delete-job" data-id="' + j.id + '">' + ico('trash') + 'Delete</button></div></div>' +
      (j.lastStatus === 'failed' ? '<div class="mb16">' + C.banner('err', 'Last run failed', esc(j.error || 'The provider rejected the update.') + ' ' + '<a href="#/executions" style="color:var(--rust);text-decoration:underline">See execution log</a>') + '</div>' : '') +
      (j.note ? '<div class="mb16">' + C.banner('info', 'Waiting for data', esc(j.note)) + '</div>' : '') +
      '<div class="grid split">' + details + targets + '</div>' +
      '<div class="mt16">' + history + '</div>';
  };
})();
