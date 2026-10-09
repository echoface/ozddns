/* OzDDNS — executions / log view */
(function () {
  var OZ = window.OZ, ico = window.ico, U = OZ.util, esc = U.esc, C = OZ.c;
  var t = function (k, v) { return OZ.t(k, v); };
  var STATUS = { all: 'All', success: 'Success', nochange: 'No change', failed: 'Failed', skipped: 'Skipped' };

  OZ.actions['set-run-filter'] = function (el) {
    OZ.state.runFilter.status = el.getAttribute('data-value'); OZ.notify();
  };
  OZ.actions['set-run-job'] = function (el) {
    OZ.state.runFilter.job = el.value; OZ.notify();
  };

  OZ.views.executions = function () {
    var D = OZ.data, rf = OZ.state.runFilter;
    var list = D.runs.filter(function (r) {
      if (rf.status !== 'all' && r.status !== rf.status) return false;
      if (rf.job !== 'all' && r.jobId !== rf.job) return false;
      return true;
    }).sort(function (a, b) { return b.startedAt - a.startedAt; });
    var counts = ['all', 'success', 'nochange', 'failed', 'skipped'].map(function (s) {
      return [s, s === 'all' ? D.runs.length : D.runs.filter(function (r) { return r.status === s; }).length];
    });

    var rows = list.map(function (r) {
      var j = OZ.get.job(r.jobId);
      return '<tr data-act="open-modal" data-modal="run-detail" data-id="' + r.id + '">' +
        '<td class="mono tiny" style="white-space:nowrap">' + esc(U.fmtDateTime(r.startedAt)) + '</td>' +
        '<td><span class="row-title">' + esc(j ? j.name : r.jobId) + '</span></td>' +
        '<td class="col-trigger">' + C.pill(r.trigger === 'manual' ? 'info' : 'mute', t(r.trigger === 'manual' ? 'manual' : 'schedule')) + '</td>' +
        '<td>' + (r.oldIp || r.newIp ? C.diff(r.oldIp, r.newIp) : '<span class="muted tiny">—</span>') + '</td>' +
        '<td class="mono tiny col-duration">' + esc(U.fmtDur(r.durationMs)) + '</td>' +
        '<td>' + C.runPill(r.status) + '</td>' +
        '<td class="muted"><span class="cell-detail tiny" title="' + esc(r.message || '') + '">' + esc(r.message || '') + '</span></td></tr>';
    }).join('');

    // The log is a rolling window: say how far back it goes instead of letting
    // the detail column sprawl.
    var days = (D.settings && D.settings.general && D.settings.general.runRetentionDays) || 1;
    var window = days === 1 ? t('last 24 hours') : t('last {n} days', { n: days });
    var head = '<div class="page-head"><div class="titles"><div class="eyebrow">Observability</div>' +
      '<h1 class="page-title">Executions</h1>' +
      '<p class="page-sub">' + esc(t('Every reconciliation the engine attempted, with the exact diff it applied. Click a row to inspect the API exchange.')) + ' ' +
      esc(t('History is kept for {win} and purged hourly.', { win: window })) + '</p></div></div>';

    var stats = '<div class="grid cols-4 mb16">' +
      C.stat('Total runs', D.runs.length, window, 'activity') +
      C.stat('Writes', counts[1][1], 'records updated', 'check-circle') +
      C.stat('No-ops', counts[2][1], 'already in sync', 'circle-dot') +
      C.stat('Failures', counts[3][1], 'need review', 'x-circle') + '</div>';

    // One flat filter bar (never a card nested in a card) that wraps instead of
    // pushing the row wider than the card it lives in.
    var filters = '<div class="card-head" style="flex-wrap:wrap;gap:10px;row-gap:8px">' +
      '<div class="seg" style="flex-wrap:wrap">' + counts.map(function (c) {
        return '<button class="' + (rf.status === c[0] ? 'on' : '') + '" data-act="set-run-filter" data-value="' + c[0] + '">' + esc(t(STATUS[c[0]] || c[0])) + ' · ' + c[1] + '</button>';
      }).join('') + '</div><span class="spacer"></span>' +
      '<select class="input" style="width:auto;max-width:100%" data-field="runJob" onchange="OZ.actions[\'set-run-job\'](this)">' +
      '<option value="all">All tasks</option>' + D.jobs.map(function (j) { return '<option value="' + j.id + '"' + (rf.job === j.id ? ' selected' : '') + '>' + esc(j.name) + '</option>'; }).join('') + '</select></div>';

    // The table scrolls inside its own wrapper so it can never spill past the
    // card background, whatever the column widths add up to.
    return head + stats + '<div class="card" style="border-top:0">' + filters +
      '<div class="table-wrap"><table class="table clickable exec-table"><thead><tr><th>Time</th><th>Task</th><th class="col-trigger">Trigger</th><th>Change</th><th class="col-duration">Duration</th><th>Result</th><th>Detail</th></tr></thead>' +
      '<tbody>' + (rows || '<tr><td colspan="7">' + C.empty('filter', 'No runs match', 'Adjust the filters above to see executions.') + '</td></tr>') + '</tbody></table></div></div>';
  };
})();
