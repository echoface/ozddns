/* ============================================================
   OzDDNS — application shell, router, modals, toasts, bootstrap
   ============================================================ */
(function () {
  var OZ = window.OZ, ico = window.ico, U = OZ.util, esc = U.esc, C = OZ.c, V = OZ.views, D = OZ.data;
  var t = function (k, v) { return OZ.t(k, v); };

  /* extra generic actions */
  OZ.actions['go'] = function (el) { OZ.navigate(el.getAttribute('data-href')); };
  OZ.actions['noop'] = function (el) {
    if (el && el.classList.contains('switch')) { el.classList.toggle('on'); return; }
    OZ.toast('info', t('Prototype'), t('This control is illustrative in the prototype.'), 2200);
  };
  OZ.actions['toggle-theme'] = function () { OZ.theme.toggle(); };
  OZ.actions['switch'] = function (el) {
    el.classList.toggle('on');
    el.setAttribute('aria-pressed', String(el.classList.contains('on')));
    var key = el.getAttribute('data-field');
    if (key) OZ.form[key] = el.classList.contains('on');
  };
  OZ.actions['save-mock'] = function () { OZ.toast('ok', t('Saved'), t('Changes persisted to the local database.')); };
  OZ.actions['set-filter'] = function (el) { var p = {}; p[el.getAttribute('data-key')] = el.getAttribute('data-value'); OZ.set(p); };
  OZ.actions['peek'] = function (el) { var i = el.previousElementSibling; i.type = i.type === 'password' ? 'text' : 'password'; };
  OZ.actions['pw-pick'] = function (el) { OZ.set({ modal: { type: 'provider-wizard', step: 1, typeId: el.getAttribute('data-id') } }); };
  OZ.actions['pw-back'] = function () { OZ.set({ modal: { type: 'provider-wizard', step: 0 } }); };
  OZ.actions['pw-create'] = function () {
    var t = OZ.get.providerType(OZ.state.modal.typeId);
    OZ.data.providers.push({ id: 'p-' + Date.now().toString(36), name: (OZ.form['pw_name'] || 'New provider'), type: t.id,
      enabled: true, status: 'ok', statusText: 'Validated just now', zones: ['example.com'], records: 0,
      config: {}, lastValidated: Date.now(), createdAt: Date.now() });
    OZ.set({ modal: null });
    OZ.toast('ok', OZ.t('Provider added'), OZ.t('{label} validated and ready.', { label: t.label }));
  };

  /* ---------- input capture ---------- */
  document.addEventListener('input', function (e) {
    var el = e.target.closest('[data-field]');
    if (el) OZ.form[el.getAttribute('data-field')] = el.value;
    // The reveal button only makes sense once there is something to reveal.
    var grp = e.target.closest ? e.target.closest('.input-group') : null;
    if (grp) {
      var pk = grp.querySelector('.peek');
      if (pk) pk.classList.toggle('is-empty', !e.target.value);
    }
  });
  document.addEventListener('change', function (e) {
    var el = e.target.closest('[data-field]');
    if (el) OZ.form[el.getAttribute('data-field')] = el.value;
  });

  /* ---------- routing ---------- */
  function currentView() {
    var s = OZ.state;
    switch (s.route) {
      case 'login': return V.login();
      case 'providers': return s.param ? V.provider(s.param.split('/')[0]) : V.providers();
      case 'jobs': return s.param === 'new' ? V.jobNew()
        : (s.param && /\/edit$/.test(s.param)) ? V.jobNew(s.param.split('/')[0])
        : (s.param ? V.job(s.param) : V.jobs());
      case 'executions': return V.executions();
      case 'settings': return V.settings();
      case 'overview': return V.overview();
      default: return V.notFound('Page not found');
    }
  }

  var NAV = [
    { k: 'overview', label: 'Overview', icon: 'gauge' },
    { k: 'providers', label: 'Providers', icon: 'server' },
    { k: 'jobs', label: 'Sync tasks', icon: 'list-checks' },
    { k: 'executions', label: 'Executions', icon: 'activity' },
    { k: 'settings', label: 'Settings', icon: 'settings' }
  ];

  function crumb() {
    var s = OZ.state, map = {
      overview: ['Control room', 'Overview'],
      providers: ['Providers', s.param ? (OZ.get.provider(s.param.split('/')[0]) || {}).name || 'Provider' : 'All providers'],
      jobs: ['Sync', s.param === 'new' ? 'New task'
        : (s.param && /\/edit$/.test(s.param)) ? 'Edit task'
        : s.param ? (OZ.get.job(s.param) || {}).name || 'Task' : 'All tasks'],
      executions: ['Observability', 'Executions'],
      settings: ['Configuration', 'Settings']
    };
    var m = map[s.route] || ['', 'Not found'];
    return '<div class="crumb"><span>' + esc(m[0]) + '</span><span class="sep">/</span><b>' + esc(m[1]) + '</b></div>';
  }

  function sidebar() {
    var nFail = D.jobs.filter(function (j) { return j.lastStatus === 'failed'; }).length;
    return '<aside class="sidebar">' +
      '<div class="brand"><span class="mark">O</span><span><span class="name">Oz<span>DDNS</span></span><span class="sub">dynamic DNS</span></span></div>' +
      '<div class="nav-group">' + NAV.map(function (n) {
        var on = OZ.state.route === n.k;
        var count = n.k === 'providers' ? D.providers.length : n.k === 'jobs' ? D.jobs.length : null;
        return '<a class="nav-item ' + (on ? 'active' : '') + '" href="#/' + n.k + '">' + ico(n.icon) +
          '<span>' + n.label + '</span>' +
          (n.k === 'jobs' && nFail ? '<span class="count warn">' + nFail + '</span>' : count != null ? '<span class="count">' + count + '</span>' : '') +
          '</a>';
      }).join('') + '</div>' +
      '<div class="sidebar-foot">' +
      '<div class="sched"><div class="row">' + ico(OZ.state.scheduler === 'running' ? 'zap' : 'pause') +
      '<span class="big">Scheduler</span><span class="spacer grow"></span>' +
      '<button class="switch ' + (OZ.state.scheduler === 'running' ? 'on' : '') + '" data-act="toggle-scheduler"></button></div>' +
      '<div class="meta">' + (OZ.state.scheduler === 'running'
        ? esc(t('Running · uptime {u}', { u: U.fmtUptime(D.runtime.uptimeSeconds) }))
        : esc(t('Paused — no automatic runs'))) + '</div></div>' +
      '<a class="who" href="#/settings/about">' + '<span class="avatar">AD</span><span class="grow"><span class="strong" style="font-size:13px">Administrator</span><br><span class="tiny muted">' + esc(D.runtime.version) + '</span></span>' + ico('chevron-right', 'muted') + '</a>' +
      '</div></aside>';
  }

  function topbar() {
    return '<header class="topbar">' +
      '<button class="icon-btn mobile-toggle" data-act="nav-toggle">' + ico('list') + '</button>' +
      crumb() + '<span class="spacer"></span>' +
      '<div class="searchbox"><span class="ico" style="display:grid;width:16px;height:16px">' + ico('search') + '</span>' +
      '<input id="globalSearch" autocomplete="off" spellcheck="false" placeholder="' +
      esc(t('Search tasks, domains, providers, sources…')) + '"><kbd>/</kbd></div>' +
      '<button class="icon-btn" data-act="noop" title="Notifications">' + ico('bell') + '</button>' +
      '<button class="icon-btn theme-toggle" data-act="toggle-theme" title="' + esc(t('Theme')) + '" aria-label="' + esc(t('Theme')) + '">' + ico(OZ.theme.isDark() ? 'sun' : 'moon') + '</button>' +
      '</header>';
  }

  /* ---------- modals ---------- */
  function renderModal() {
    var m = OZ.state.modal;
    if (!m) return '';
    var sheet;
    if (m.type === 'confirm') {
      sheet = '<div class="modal"><div class="sheet" style="width:min(440px,100%)">' +
        '<div class="sheet-head"><span class="logo lg" style="color:var(--rust);background:var(--rust-050)">' + ico('alert-triangle') + '</span>' +
        '<div><div class="h">' + esc(m.title) + '</div><div class="s">' + esc(m.body) + '</div></div></div>' +
        '<div class="sheet-foot"><button class="btn" data-act="close-modal">Cancel</button>' +
        '<button class="btn ' + (m.danger ? 'danger' : 'primary') + '" data-act="confirm-modal">' + esc(m.confirm) + '</button></div></div></div>';
    } else if (m.type === 'run') {
      var r = m.run, j = OZ.get.job(r.jobId);
      var steps = [
        ['Read IP source', r.newIp ? 'resolved ' + r.newIp : 'no address available'],
        ['Fetch current record', r.oldIp ? 'found ' + r.oldIp : 'no existing record'],
        ['Compute diff', r.oldIp === r.newIp ? 'in sync — no write' : 'drift detected'],
        [r.status === 'failed' ? 'Write rejected' : r.status === 'success' ? 'Write applied' : 'Reconcile', r.message || 'done']
      ];
      sheet = '<div class="modal"><div class="sheet wide">' +
        '<div class="sheet-head"><span class="logo lg" style="color:var(--slate);background:var(--slate-050)">' + ico('activity') + '</span>' +
        '<div class="grow"><div class="h">' + esc(t('Execution {id}', { id: r.id })) + '</div><div class="s">' + esc(j ? j.name : r.jobId) + ' · ' + esc(U.fmtDateTime(r.startedAt)) + ' · ' + esc(r.trigger) + '</div></div>' +
        C.runPill(r.status) + '<button class="icon-btn" data-act="close-modal">' + ico('x') + '</button></div>' +
        '<div class="sheet-body">' +
        '<div class="grid cols-3 mb16">' + C.stat('Old value', '<span class="mono" style="font-size:15px">' + esc(r.oldIp || '—') + '</span>', '', 'arrow-left') +
        C.stat('New value', '<span class="mono" style="font-size:15px">' + esc(r.newIp || '—') + '</span>', '', 'arrow-right') +
        C.stat('Duration', esc(U.fmtDur(r.durationMs)), '', 'clock') + '</div>' +
        '<div class="strong mb8">Steps</div><div class="timeline mb16">' + steps.map(function (s, i, a) {
          return '<div class="tl-item"><div class="tl-rail"><span class="tl-node ' + (r.status === 'failed' && i === 3 ? 'err' : r.status === 'success' && i === 3 ? 'ok' : 'info') + '"></span>' + (i < a.length - 1 ? '<span class="tl-line"></span>' : '') + '</div>' +
            '<div class="tl-body"><div class="tl-text"><b>' + esc(s[0]) + '</b></div><div class="tl-text muted">' + esc(s[1]) + '</div></div></div>';
        }).join('') + '</div>' +
        (r.status === 'failed' ? C.banner('err', 'Provider rejected the request', esc(r.message || '')) : '') +
        '<div class="strong mb8 mt16">Provider exchange</div>' +
        '<div class="code"><span class="c"># ' + esc(OZ.get.providerType(OZ.get.provider(j.providerId).type).label) + '</span>\n' +
        '<span class="k">GET</span> /zones/•••/dns_records?type=' + (j.targets[0].type) + '\n' +
        '<span class="s">→ 200 OK</span>\n' +
        (r.status === 'success' ? '<span class="k">PUT</span> /zones/•••/dns_records/•••\n<span class="s">→ 200 OK</span>' : r.status === 'failed' ? '<span class="k">PUT</span> /zones/•••/dns_records/•••\n<span class="s" style="color:#E7A896">→ 401 Unauthorized</span>' : '<span class="c"># no write — desired already matches</span>') + '</div>' +
        '</div>' +
        '<div class="sheet-foot"><button class="btn" data-act="run-job" data-id="' + r.jobId + '">' + ico('refresh') + 'Re-run</button>' +
        '<button class="btn primary" data-act="close-modal">Close</button></div></div></div>';
    } else if (m.type === 'provider-wizard') {
      if (m.step === 0) {
        var body = '<div class="pick-grid">' + D.providerTypes.map(function (t) {
          return '<button class="pick" data-act="pw-pick" data-id="' + t.id + '">' +
            C.providerLogo(t.id) + '<span><span class="t">' + esc(t.label) + '</span><span class="d">' + esc(t.desc) + '</span></span></button>';
        }).join('') + '</div>';
        sheet = '<div class="modal"><div class="sheet wide"><div class="sheet-head"><div class="grow"><div class="h">Add a provider</div>' +
          '<div class="s">Choose the DNS provider you want OzDDNS to write to.</div></div><button class="icon-btn" data-act="close-modal">' + ico('x') + '</button></div>' +
          '<div class="sheet-body">' + body + '</div></div></div>';
      } else {
        var t = OZ.get.providerType(m.typeId);
        var fields = C.field({ key: 'pw_name', field: { type: 'text', label: 'Display name', required: true }, placeholder: OZ.t('{label} (production)', { label: t.label }) }) +
          (t.fields || []).map(function (f) { return '<div data-scope="pw">' + C.field({ field: f, key: 'pw_' + f.key }) + '</div>'; }).join('');
        sheet = '<div class="modal"><div class="sheet"><div class="sheet-head">' + C.providerLogo(t.id, true) +
          '<div class="grow"><div class="h">' + esc(t.label) + '</div><div class="s">' + esc(t.desc) + '</div></div><button class="icon-btn" data-act="close-modal">' + ico('x') + '</button></div>' +
          '<div class="sheet-body">' + fields + C.banner('info', 'Credentials are encrypted at rest', 'They are masked in the UI and redacted from logs and API responses.') + '</div>' +
          '<div class="sheet-foot"><button class="btn" data-act="pw-back">' + ico('arrow-left') + 'Back</button>' +
          '<button class="btn primary" data-act="pw-create">' + ico('check') + 'Add &amp; validate</button></div></div></div>';
      }
    } else if (m.type === 'source') {
      var sEdit = m.source || null;
      var sUsers = sEdit ? D.jobs.filter(function (j) { return j.ipSourceId === sEdit.id; }).length : 0;
      var ifaceOpts = (D.interfaces || []).map(function (ifc) {
        return (ifc.addresses || []).map(function (a) {
          return '<option value="' + esc(a.target) + '">' + esc(ifc.name + ' · ' + a.addr) + '</option>';
        }).join('');
      }).join('');
      // Type and family are chosen by label, not by their raw ids.
      var curType = OZ.form['ns_type'] || (sEdit ? sEdit.type : 'http');
      var curFamily = OZ.form['ns_family'] || (sEdit ? sEdit.family : 'v4');
      var typeSelect = '<div class="field"><label>' + esc(OZ.t('Type')) + '</label>' +
        '<select class="input" data-field="ns_type">' + D.ipSourceTypes.map(function (x) {
          return '<option value="' + esc(x.id) + '"' + (x.id === curType ? ' selected' : '') + '>' + esc(OZ.t(x.label)) + '</option>';
        }).join('') + '</select></div>';
      var familySelect = '<div class="field"><label>' + esc(OZ.t('Family')) + '</label>' +
        '<select class="input" data-field="ns_family">' + ['v4', 'v6'].map(function (f) {
          return '<option value="' + f + '"' + (f === curFamily ? ' selected' : '') + '>IPv' + f.slice(1) + '</option>';
        }).join('') + '</select></div>';
      sheet = '<div class="modal"><div class="sheet" style="width:min(520px,100%)">' +
        '<div class="sheet-head"><span class="logo lg" style="color:var(--slate);background:var(--slate-050)">' + ico('globe') + '</span>' +
        '<div class="grow"><div class="h">' + esc(OZ.t(sEdit ? 'Edit IP source' : 'Add an IP source')) + '</div>' +
        '<div class="s">' + esc(OZ.t(sEdit ? 'Changes apply to every task that uses this source.' : "Tell OzDDNS where this host's address comes from.")) + '</div></div>' +
        '<button class="icon-btn" data-act="close-modal">' + ico('x') + '</button></div>' +
        '<div class="sheet-body">' +
        (sUsers ? C.banner('warn', OZ.t('Shared with {n} task(s)', { n: sUsers }),
          esc(OZ.t('They read this source on their next run, so the change lands immediately. The address family is locked while tasks reference it.'))) : '') +
        C.field({ key: 'ns_name', field: { type: 'text', label: 'Name', required: true }, placeholder: 'ipify (IPv4)' }) +
        typeSelect + familySelect +
        C.field({ key: 'ns_target', field: { type: 'text', label: 'Target', mono: true, required: true }, placeholder: 'https://api.ipify.org', list: 'ns-iface-targets' }) +
        '<datalist id="ns-iface-targets">' + ifaceOpts + '</datalist>' +
        '</div>' +
        '<div class="sheet-foot"><button class="btn" data-act="close-modal">Cancel</button>' +
        '<button class="btn primary" data-act="' + (sEdit ? 'save-source' : 'add-source') + '"' +
        (sEdit ? ' data-id="' + esc(sEdit.id) + '"' : '') + '>' + ico('check') +
        esc(OZ.t(sEdit ? 'Save source' : 'Add source')) + '</button></div></div></div>';
    } else if (m.type === 'notify') {
      sheet = '<div class="modal"><div class="sheet" style="width:min(520px,100%)">' +
        '<div class="sheet-head"><span class="logo lg" style="color:var(--coral-700);background:var(--coral-050)">' + ico('bell') + '</span>' +
        '<div class="grow"><div class="h">Add a notification channel</div><div class="s">OzDDNS POSTs a JSON payload on the events you choose.</div></div>' +
        '<button class="icon-btn" data-act="close-modal">' + ico('x') + '</button></div>' +
        '<div class="sheet-body">' +
        C.field({ key: 'nn_name', field: { type: 'text', label: 'Name', required: true }, placeholder: '#homelab alerts' }) +
        C.field({ key: 'nn_kind', field: { type: 'select', label: 'Kind', options: ['slack', 'discord', 'ntfy', 'gotify', 'webhook'] }, value: 'webhook' }) +
        C.field({ key: 'nn_target', field: { type: 'text', label: 'Webhook URL', mono: true, required: true }, placeholder: 'https://hooks.slack.com/services/…' }) +
        C.field({ key: 'nn_on', field: { type: 'select', label: 'Notify on', options: ['change', 'failure', 'success', 'all'] }, value: 'failure' }) +
        '</div>' +
        '<div class="sheet-foot"><button class="btn" data-act="close-modal">Cancel</button>' +
        '<button class="btn primary" data-act="add-notify">' + ico('check') + 'Add channel</button></div></div></div>';
    } else if (m.type === 'token') {
      sheet = '<div class="modal"><div class="sheet" style="width:min(500px,100%)">' +
        '<div class="sheet-head"><span class="logo lg" style="color:var(--coral-700);background:var(--coral-050)">' + ico('key') + '</span>' +
        '<div><div class="h">Create API token</div><div class="s">Use a scoped token for the CLI and scripts.</div></div></div>' +
        '<div class="sheet-body">' + C.field({ key: 'tok_name', field: { type: 'text', label: 'Token name' }, placeholder: 'ci-cron' }) +
        '<div class="callout mb16">Copy this now — it will not be shown again.<div class="code mt8" style="font-size:12px">oz_tok_9f3a2e7c<b>d41b</b>6f0e2a88c7</div></div></div>' +
        '<div class="sheet-foot"><button class="btn" data-act="close-modal">Cancel</button>' +
        '<button class="btn primary" data-act="close-modal">' + ico('check') + 'Create token</button></div></div></div>';
    }
    return '<div class="scrim" data-act="close-modal"></div>' + sheet;
  }

  function renderToasts() {
    if (!OZ.state.toasts.length) return '';
    return '<div class="toasts">' + OZ.state.toasts.map(function (t) {
      return '<div class="toast ' + t.kind + '">' + ico(t.kind === 'ok' ? 'check-circle' : t.kind === 'err' ? 'x-circle' : 'info') +
        '<div class="grow"><div class="t">' + esc(t.title) + '</div>' + (t.desc ? '<div class="d">' + esc(t.desc) + '</div>' : '') + '</div>' +
        '<button class="icon-btn" style="color:inherit;width:24px;height:24px" data-act="dismiss-toast" data-id="' + t.id + '">' + ico('x') + '</button></div>';
    }).join('') + '</div>';
  }
  OZ.actions['dismiss-toast'] = function (el) { OZ.dismissToast(el.getAttribute('data-id')); };

  /* ---------- boot skeleton ----------
     Shown until /bootstrap resolves, so the panel never paints the bundled
     mock dataset (or zero-value defaults) before the real data arrives. It
     mirrors the real shell so the swap-in is seamless. */
  function sk(w, h, r) {
    return '<span class="sk" style="width:' + w + ';height:' + h + ';border-radius:' + (r || '6px') + '"></span>';
  }
  function skeletonShell() {
    var nav = ['96px', '128px', '104px', '112px', '84px'].map(function (w) {
      return '<div class="nav-item sk-nav">' + sk('18px', '18px', '5px') + sk(w, '12px') + '</div>';
    }).join('');
    var tiles = [0, 1, 2].map(function () {
      return '<div class="card pad" style="min-height:118px">' + sk('62%', '12px') +
        '<div class="mt16">' + sk('84px', '26px', '8px') + '</div>' +
        '<div class="mt16">' + sk('100%', '11px') + '</div></div>';
    }).join('');
    return '<div class="app" aria-busy="true">' +
      '<aside class="sidebar">' +
      '<div class="brand"><span class="mark">O</span><span><span class="name">Oz<span>DDNS</span></span><span class="sub">dynamic DNS</span></span></div>' +
      '<div class="nav-group">' + nav + '</div>' +
      '<div class="sidebar-foot">' +
      '<div class="sched"><div class="row">' + sk('20px', '20px') + sk('72px', '12px') + '</div>' +
      '<div class="mt12">' + sk('100%', '10px') + '</div></div>' +
      '<div class="who">' + sk('30px', '30px', '50%') + sk('120px', '12px') + '</div>' +
      '</div></aside>' +
      '<main class="main">' +
      '<header class="topbar">' + sk('150px', '14px', '7px') + '<span class="spacer"></span>' +
      sk('224px', '34px', '999px') + sk('36px', '36px') + sk('36px', '36px') + '</header>' +
      '<div class="content">' +
      '<div class="mb24">' + sk('120px', '12px') + '<div class="mt12">' + sk('260px', '30px', '8px') + '</div>' +
      '<div class="mt12">' + sk('min(420px,80%)', '13px') + '</div></div>' +
      '<div class="grid cols-3">' + tiles + '</div>' +
      '<div class="card pad mt16" style="min-height:210px">' + sk('30%', '14px') +
      '<div class="mt16">' + sk('100%', '11px') + '</div><div class="mt12">' + sk('92%', '11px') + '</div>' +
      '<div class="mt12">' + sk('96%', '11px') + '</div><div class="mt12">' + sk('70%', '11px') + '</div></div>' +
      '</div></main></div>';
  }

  /* ---------- global search ----------
     One box in the top bar, indexed from whatever is loaded: sync tasks,
     providers, IP sources, the newest executions and the nav pages. It is a
     jump-to, not a query language — Enter opens the highlighted hit. */
  var search = { hits: [], sel: 0 };

  function searchIndex() {
    var items = [];
    D.jobs.forEach(function (j) {
      var domains = (j.targets || []).map(function (x) { return x.domain; }).join(', ');
      var prov = OZ.get.provider(j.providerId);
      items.push({ kind: 'Task', icon: 'jobs', label: j.name,
        sub: domains || (prov ? prov.name : ''), go: function () { OZ.navigate('#/jobs/' + j.id); } });
    });
    D.providers.forEach(function (p) {
      var ty = OZ.get.providerType(p.type) || {};
      items.push({ kind: 'Provider', icon: 'server', label: p.name,
        sub: (ty.label || p.type) + ((p.zones || []).length ? ' · ' + p.zones.join(', ') : ''),
        go: function () { OZ.navigate('#/providers/' + p.id); } });
    });
    D.ipSources.forEach(function (s) {
      items.push({ kind: 'IP source', icon: 'globe', label: s.name, sub: s.target,
        go: function () { OZ.navigate('#/settings/sources'); } });
    });
    D.runs.slice(0, 30).forEach(function (r) {
      var j = OZ.get.job(r.jobId);
      var diff = r.oldIp && r.newIp && r.oldIp !== r.newIp ? r.oldIp + ' → ' + r.newIp : '';
      items.push({ kind: 'Execution', icon: 'activity',
        label: (j ? j.name : r.jobId) + ' · ' + U.fmtDateTime(r.startedAt),
        sub: r.message || diff, go: function () { OZ.set({ modal: { type: 'run', run: r } }); } });
    });
    NAV.forEach(function (n) {
      items.push({ kind: 'Page', icon: n.icon, label: n.label, sub: '',
        go: function () { OZ.navigate('#/' + n.k); } });
    });
    return items;
  }

  function searchRun(q) {
    q = (q || '').trim().toLowerCase();
    if (!q) return [];
    return searchIndex().filter(function (it) {
      return (it.label + ' ' + (it.sub || '') + ' ' + it.kind).toLowerCase().indexOf(q) > -1;
    }).slice(0, 8);
  }

  function searchPanelEl() {
    var box = document.querySelector('.searchbox');
    if (!box) return null;
    var p = box.querySelector('#searchPanel');
    if (!p) {
      p = document.createElement('div');
      p.id = 'searchPanel';
      p.className = 'search-panel';
      // mousedown beats the input's focusout, so a click can still register.
      p.addEventListener('mousedown', function (e) {
        var hit = e.target.closest ? e.target.closest('.search-hit') : null;
        if (!hit) return;
        e.preventDefault();
        searchGo(parseInt(hit.getAttribute('data-idx'), 10));
      });
      box.appendChild(p);
    }
    return p;
  }

  function searchRemove() {
    var p = document.getElementById('searchPanel');
    if (p && p.parentNode) p.parentNode.removeChild(p);
  }

  function searchRender() {
    var input = document.getElementById('globalSearch');
    if (!input) return;
    var q = input.value;
    if (!q.trim()) { searchRemove(); return; }
    var panel = searchPanelEl();
    if (!panel) return;
    var hits = searchRun(q);
    search.hits = hits;
    if (search.sel >= hits.length) search.sel = 0;
    if (!hits.length) {
      panel.innerHTML = '<div class="search-none">' + esc(t('Nothing matches “{q}”.', { q: q })) + '</div>';
      return;
    }
    panel.innerHTML = hits.map(function (it, i) {
      return '<button type="button" class="search-hit ' + (i === search.sel ? 'on' : '') + '" data-idx="' + i + '">' +
        '<span class="ico">' + ico(it.icon) + '</span>' +
        '<span class="grow"><span class="t">' + esc(it.label) + '</span>' +
        (it.sub ? '<span class="d">' + esc(it.sub) + '</span>' : '') + '</span>' +
        '<span class="pill mute">' + esc(t(it.kind)) + '</span></button>';
    }).join('');
  }

  function searchGo(idx) {
    var it = (search.hits || [])[idx];
    searchRemove();
    var input = document.getElementById('globalSearch');
    if (input) { input.value = ''; input.blur(); }
    search.sel = 0;
    if (it) { it.go(); OZ.notify(); }
  }

  function searchKey(e) {
    var input = document.getElementById('globalSearch');
    if (!input || document.activeElement !== input) return false;
    if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
      var hits = search.hits || [];
      if (hits.length) {
        search.sel = (search.sel + (e.key === 'ArrowDown' ? 1 : hits.length - 1)) % hits.length;
        searchRender();
      }
      return true;
    }
    if (e.key === 'Enter') { searchGo(search.sel); return true; }
    if (e.key === 'Escape') { searchRemove(); input.blur(); return true; }
    return false;
  }

  document.addEventListener('input', function (e) {
    if (e.target && e.target.id === 'globalSearch') { search.sel = 0; searchRender(); }
  });
  document.addEventListener('focusout', function (e) {
    if (e.target && e.target.id === 'globalSearch') setTimeout(searchRemove, 120);
  });

  /* ---------- render ---------- */
  // Data tables keep their shape instead of wrapping: optional text labels
  // (the source type, the "Test" caption) drop out when a row no longer fits,
  // and horizontal scrolling is only the last resort. Each render starts from
  // clean markup, so the measurement is always against the current width.
  function compactTables(root) {
    var wraps = root.querySelectorAll('.table-wrap');
    for (var i = 0; i < wraps.length; i++) {
      if (wraps[i].scrollWidth > wraps[i].clientWidth + 1) wraps[i].classList.add('is-compact');
    }
  }

  function render() {
    var root = document.getElementById('root');
    var s = OZ.state;
    if (s.boot === 'loading') {
      root.innerHTML = skeletonShell();
      OZ.i18n.applyDOM(root);
      return;
    }
    if (s.route === 'login') {
      root.innerHTML = '<div class="app login-mode" style="display:block">' + currentView() + renderToasts() + '</div>';
      OZ.i18n.applyDOM(root);
      compactTables(root);
      return;
    }
    if (s.modal && s.modal.type) { /* keep nav open */ }
    root.innerHTML =
      '<div class="app ' + (s.navOpen ? 'nav-open' : '') + '">' + sidebar() +
      '<div class="nav-scrim" data-act="nav-close" aria-hidden="true"></div>' +
      '<main class="main">' + topbar() + '<div class="content">' + currentView() + '</div></main></div>' +
      renderModal() + renderToasts();
    OZ.i18n.applyDOM(root);
    compactTables(root);
  }

  OZ.subscribe(render);

  /* ---------- boot ---------- */
  var start = OZ.parseHash(window.location.hash || '#/overview');
  OZ.state.route = start.route; OZ.state.param = start.param;
  if (!window.location.hash) window.location.hash = '#/overview';
  render();

  /* periodic "live" feel: refresh timestamps sparingly */
  setInterval(function () {
    if (OZ.state.route === 'executions' || OZ.state.route === 'overview') {
      // no-op visual tick; keeps relative times honest if the tab stays open
    }
  }, 15000);

  document.addEventListener('keydown', function (e) {
    if (searchKey(e)) { e.preventDefault(); return; }
    if (e.key === '/' && document.activeElement.tagName !== 'INPUT' && document.activeElement.tagName !== 'TEXTAREA') {
      var s = document.getElementById('globalSearch'); if (s) { e.preventDefault(); s.focus(); }
    }
    if (e.key === 'Escape') {
      if (OZ.state.modal) OZ.set({ modal: null });
      else if (OZ.state.navOpen) OZ.set({ navOpen: false });
    }
  });
})();
