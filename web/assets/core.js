/* ============================================================
   OzDDNS — core: utilities, state store, hash router, toasts
   ============================================================ */
(function () {
  var OZ = window.OZ = window.OZ || {};

  /* ---------------- utils ---------------- */
  var U = OZ.util = {
    esc: function (s) {
      return String(s == null ? '' : s).replace(/[&<>"']/g, function (c) {
        return { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c];
      });
    },
    fmtTime: function (ts) {
      if (!ts) return '—';
      var d = new Date(ts);
      return d.toLocaleTimeString(OZ.i18n.locale(), { hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false });
    },
    fmtDateTime: function (ts) {
      if (!ts) return '—';
      var d = new Date(ts);
      return d.toLocaleDateString(OZ.i18n.locale(), { month: 'short', day: 'numeric' }) + ' ' +
        d.toLocaleTimeString(OZ.i18n.locale(), { hour: '2-digit', minute: '2-digit', hour12: false });
    },
    fmtAgo: function (ts) {
      var t = OZ.t;
      if (!ts) return t('never');
      var s = Math.max(0, (Date.now() - ts) / 1000);
      if (s < 45) return t('just now');
      if (s < 90) return t('1m ago');
      if (s < 3600) return t('{n}m ago', { n: Math.round(s / 60) });
      if (s < 5400) return t('1h ago');
      if (s < 86400) return t('{n}h ago', { n: Math.round(s / 3600) });
      return t('{n}d ago', { n: Math.round(s / 86400) });
    },
    fmtIn: function (ts) {
      var t = OZ.t;
      if (!ts) return t('paused');
      var s = (ts - Date.now()) / 1000;
      if (s <= 0) return t('due now');
      if (s < 60) return t('in {n}s', { n: Math.round(s) });
      if (s < 3600) return t('in {n}m', { n: Math.round(s / 60) });
      return t('in {n}h', { n: (s / 3600).toFixed(1) });
    },
    fmtDur: function (ms) {
      if (ms == null) return '—';
      if (ms < 1000) return ms + 'ms';
      return (ms / 1000).toFixed(ms < 10000 ? 2 : 1) + 's';
    },
    fmtUptime: function (sec) {
      var d = Math.floor(sec / 86400), h = Math.floor(sec % 86400 / 3600), m = Math.floor(sec % 3600 / 60);
      return (d ? d + 'd ' : '') + h + 'h ' + m + 'm';
    },
    plural: function (n, s, p) { return n + ' ' + (n === 1 ? s : (p || s + 's')); }
  };

  /* ---------------- store ---------------- */
  var D = OZ.data;
  var state = OZ.state = {
    // 'loading' → waiting for /bootstrap (render a skeleton, never the mock
    // defaults); 'ready' → live data; 'offline' → server unreachable, use the
    // bundled mock dataset so the prototype still opens from the filesystem.
    boot: 'loading',
    route: 'overview',
    param: null,
    navOpen: false,
    scheduler: 'running',
    toasts: [],
    modal: null,
    drawer: null,
    providerFilter: 'all',
    runFilter: { job: 'all', status: 'all' },
    authPasswordError: false,
    lastTick: Date.now(),
    data: D
  };

  var subs = [];
  OZ.form = {}; // lightweight field capture (prototype only)
  OZ.subscribe = function (fn) { subs.push(fn); };
  OZ.notify = function () { subs.forEach(function (fn) { fn(state); }); };
  OZ.set = function (patch) { for (var k in patch) state[k] = patch[k]; OZ.notify(); };

  OZ.get = {
    provider: function (id) { return D.providers.filter(function (p) { return p.id === id; })[0]; },
    providerType: function (id) { return D.providerTypes.filter(function (p) { return p.id === id; })[0]; },
    job: function (id) { return D.jobs.filter(function (j) { return j.id === id; })[0]; },
    ipSource: function (id) { return D.ipSources.filter(function (s) { return s.id === id; })[0]; },
    runsFor: function (jobId) { return jobId ? D.runs.filter(function (r) { return r.jobId === jobId; }) : D.runs; },
    run: function (id) { return D.runs.filter(function (r) { return r.id === id; })[0]; }
  };

  /* ---------------- toasts ---------------- */
  var tseq = 1;
  OZ.toast = function (kind, title, desc, ms) {
    var id = 't' + (tseq++);
    state.toasts.push({ id: id, kind: kind, title: title, desc: desc || '' });
    OZ.notify();
    setTimeout(function () {
      state.toasts = state.toasts.filter(function (t) { return t.id !== id; });
      OZ.notify();
    }, ms || 4200);
  };
  OZ.dismissToast = function (id) {
    state.toasts = state.toasts.filter(function (t) { return t.id !== id; });
    OZ.notify();
  };

  /* ---------------- router ---------------- */
  OZ.navigate = function (hash) { window.location.hash = hash; };

  function parse(hash) {
    var h = (hash || '').replace(/^#\/?/, '');
    var parts = h.split('/').filter(Boolean);
    var route = parts[0] || 'overview';
    var param = parts.slice(1).join('/') || null;
    return { route: route, param: param };
  }
  OZ.parseHash = parse;

  window.addEventListener('hashchange', function () {
    var r = parse(window.location.hash);
    state.route = r.route; state.param = r.param; state.navOpen = false;
    state.modal = null; state.drawer = null;
    window.scrollTo(0, 0);
    OZ.notify();
  });

  /* ---------------- mutations (mock backend) ---------------- */
  var t = function (k, v) { return OZ.t(k, v); };

  // The IP-source sheet is shared by "add" and "edit", so its fields have to be
  // wiped before it opens — otherwise a previous edit leaks into the next add.
  function clearSourceForm() {
    ['ns_name', 'ns_type', 'ns_family', 'ns_target'].forEach(function (k) { delete OZ.form[k]; });
  }
  OZ.clearSourceForm = clearSourceForm;

  OZ.actions = {
    'toggle-scheduler': function () {
      state.scheduler = state.scheduler === 'running' ? 'paused' : 'running';
      OZ.toast('ok', t(state.scheduler === 'running' ? 'Scheduler resumed' : 'Scheduler paused'),
        t(state.scheduler === 'running' ? 'Jobs will run on schedule.' : 'No automatic runs until resumed.'));
    },
    'toggle-job': function (el) {
      var j = OZ.get.job(el.getAttribute('data-id'));
      j.enabled = !j.enabled;
      j.nextRun = j.enabled ? Date.now() + j.intervalMinutes * 60000 : null;
      OZ.toast(j.enabled ? 'ok' : 'info', t(j.enabled ? 'Enabled {name}' : 'Paused {name}', { name: j.name }));
      OZ.notify();
    },
    'toggle-provider': function (el) {
      var p = OZ.get.provider(el.getAttribute('data-id'));
      p.enabled = !p.enabled;
      OZ.toast(p.enabled ? 'ok' : 'info', t(p.enabled ? 'Enabled {name}' : 'Disabled {name}', { name: p.name }));
      OZ.notify();
    },
    'run-job': function (el) {
      var j = OZ.get.job(el.getAttribute('data-id'));
      OZ.toast('info', t('Running {name}…', { name: j.name }),
        t('Reconciling records against {provider}.', { provider: OZ.get.providerType(OZ.get.provider(j.providerId).type).label }), 2600);
      setTimeout(function () {
        var r = { id: 'r-' + (100 + OZ.data.runs.length), jobId: j.id, startedAt: Date.now(), durationMs: 620,
          status: j.lastStatus === 'failed' ? 'failed' : 'nochange', oldIp: j.current, newIp: j.desired, trigger: 'manual',
          message: t('Manual run completed.') };
        OZ.data.runs.unshift(r);
        j.lastRun = Date.now();
        OZ.toast(j.lastStatus === 'failed' ? 'err' : 'ok',
          j.lastStatus === 'failed' ? t('{name} failed', { name: j.name }) : t('{name} · no change', { name: j.name }),
          j.lastStatus === 'failed' ? (j.error || t('Update failed')) : t('Records already match the desired IP.'));
        OZ.notify();
      }, 900);
    },
    'run-all': function () {
      OZ.toast('info', t('Reconciling all enabled jobs…'), t('Reading current records and diffing.'), 2600);
      setTimeout(function () {
        OZ.toast('ok', t('All jobs reconciled'), t('{success} updated · {nochange} unchanged · {skipped} skipped · {failed} failed', { success: 1, nochange: 5, skipped: 0, failed: 1 }));
        OZ.notify();
      }, 1100);
    },
    'refresh-ip': function () {
      OZ.data.ipInfo.v4Checked = Date.now();
      OZ.data.ipInfo.v6Checked = Date.now();
      OZ.toast('ok', t('IP refreshed'), t('Re-checked the public address.'));
      OZ.notify();
    },
    'validate-provider': function (el) {
      var p = OZ.get.provider(el.getAttribute('data-id'));
      OZ.toast('info', t('Validating {name}…', { name: p.name }), t('Checking credentials & zones.'), 2000);
      setTimeout(function () {
        if (p.type === 'godaddy') { OZ.toast('err', t('{name} failed validation', { name: p.name }), t('Invalid credentials (401).')); }
        else { p.status = 'ok'; p.statusText = 'validated successfully'; p.lastValidated = Date.now(); OZ.toast('ok', t('{name} is valid', { name: p.name }), t('Credentials and zones look good.')); }
        OZ.notify();
      }, 900);
    },
    'delete-provider': function (el) {
      var p = OZ.get.provider(el.getAttribute('data-id'));
      OZ.set({ modal: { type: 'confirm', title: t('Delete provider?'), body: t('"{name}" will be removed. Jobs that reference it must be reassigned.', { name: p.name }), confirm: t('Delete provider'), danger: true, onConfirm: function () {
        OZ.data.providers = OZ.data.providers.filter(function (x) { return x.id !== p.id; });
        OZ.toast('ok', t('Provider deleted'), p.name);
        OZ.navigate('#/providers'); OZ.notify();
      } } });
    },
    'delete-job': function (el) {
      var j = OZ.get.job(el.getAttribute('data-id'));
      OZ.set({ modal: { type: 'confirm', title: t('Delete sync task?'), body: t('"{name}" and its history will be removed.', { name: j.name }), confirm: t('Delete task'), danger: true, onConfirm: function () {
        OZ.data.jobs = OZ.data.jobs.filter(function (x) { return x.id !== j.id; });
        OZ.toast('ok', t('Task deleted'), j.name);
        OZ.navigate('#/jobs'); OZ.notify();
      } } });
    },
    'test-webhook': function (el) {
      OZ.toast('info', t('Sending test notification…'), t('Delivering a sample payload.'), 1800);
      setTimeout(function () { OZ.toast('ok', t('Test delivered'), t('Endpoint responded 200 OK.')); }, 800);
    },
    'copy': function (el) {
      var v = el.getAttribute('data-copy');
      if (navigator.clipboard) navigator.clipboard.writeText(v).catch(function () {});
      OZ.toast('ok', t('Copied to clipboard'), v.length > 40 ? v.slice(0, 40) + '…' : v, 2000);
    },
    'reveal': function (el) {
      el.previousElementSibling.textContent = el.getAttribute('data-value');
      el.remove();
    },
    'open-modal': function (el) {
      var name = el.getAttribute('data-modal');
      if (name === 'run-detail') {
        OZ.set({ modal: { type: 'run', run: OZ.get.run(el.getAttribute('data-id')) } });
      } else if (name === 'new-provider') {
        OZ.set({ modal: { type: 'provider-wizard', step: 0 } });
      } else if (name === 'source-new') {
        clearSourceForm();
        OZ.set({ modal: { type: 'source' } });
      } else if (name === 'notify-new') {
        OZ.set({ modal: { type: 'notify' } });
      } else if (name === 'invite') {
        OZ.set({ modal: { type: 'token' } });
      }
    },
    'close-modal': function () { OZ.set({ modal: null }); },
    'confirm-modal': function () {
      var m = state.modal;
      if (m && m.onConfirm) m.onConfirm();
      OZ.set({ modal: null });
    },
    'nav-toggle': function () { state.navOpen = !state.navOpen; OZ.notify(); },
    'nav-close': function () { if (state.navOpen) { state.navOpen = false; OZ.notify(); } },
    'logout': function () { OZ.navigate('#/login'); }
  };

  /* delegated click handler */
  document.addEventListener('click', function (e) {
    var el = e.target.closest('[data-act]');
    if (!el) return;
    // A <select>/<input> must not fire its action on click: preventDefault
    // (and any re-render the action causes) would close the control the moment
    // it is opened. Those fire on `change` instead, just below.
    if (el.tagName === 'SELECT' || el.tagName === 'INPUT') return;
    var fn = OZ.actions[el.getAttribute('data-act')];
    if (fn) { e.preventDefault(); fn(el, e); }
  });
  /* selects/inputs that drive a re-render action fire on change, not click */
  document.addEventListener('change', function (e) {
    var el = e.target.closest('select[data-act], input[data-act]');
    if (!el) return;
    var fn = OZ.actions[el.getAttribute('data-act')];
    if (fn) fn(el, e);
  });
})();
