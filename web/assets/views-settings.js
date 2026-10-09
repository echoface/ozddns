/* OzDDNS — settings view */
(function () {
  var OZ = window.OZ, ico = window.ico, U = OZ.util, esc = U.esc, C = OZ.c;
  var t = function (k, v) { return OZ.t(k, v); };
  var TABS = [['general', 'General'], ['scheduler', 'Scheduler'], ['security', 'Security'], ['sources', 'IP sources'], ['notify', 'Notifications'], ['storage', 'Storage'], ['about', 'About']];

  function saveBtn() { return '<button class="btn primary mt16" data-act="save-settings">' + ico('save') + 'Save changes</button>'; }

  // Locale picker: options carry language *codes* as values, and are kept out
  // of the DOM translator so the labels stay in their own language.
  function langField(current) {
    var norm = OZ.i18n.normalize(current) || 'en';
    var opts = OZ.i18n.codes.map(function (c) {
      return '<option value="' + c + '"' + (c === norm ? ' selected' : '') + '>' + OZ.i18n.label(c) + '</option>';
    }).join('');
    return '<div class="field" data-no-i18n><label>' + esc(t('Language')) + '</label>' +
      '<select class="input" data-field="s_lang" data-lang>' + opts + '</select></div>';
  }

  // Theme picker: values are theme *codes* (light/dark/auto); the option
  // labels are short words that the DOM translator localises.
  function themeField(current) {
    var norm = OZ.theme.normalize(current) || 'light';
    var opts = OZ.theme.codes.map(function (c) {
      return '<option value="' + c + '"' + (c === norm ? ' selected' : '') + '>' + OZ.theme.label(c) + '</option>';
    }).join('');
    return '<div class="field"><label>' + esc(t('Theme')) + '</label>' +
      '<select class="input" data-field="s_theme" data-theme-pick>' + opts + '</select>' +
      '<span class="help">' + esc(t('Auto follows your operating system setting.')) + '</span></div>';
  }

  // Scheduler card. This is the "internal timer" the user asked about: the
  // daemon wakes on a tick, reconciles every task whose interval has elapsed,
  // and these controls tune that loop live.
  function schedulerCard(s, r) {
    var running = OZ.state.scheduler !== 'paused';
    return '<div class="card pad"><div class="between mb12">' +
      '<span class="strong">Scheduler</span>' +
      C.pill(running ? 'ok' : 'mute', running ? 'Running' : 'Paused', true) + '</div>' +
      '<p class="tiny muted mb16">' + t('OzDDNS wakes on a timer, reconciles every task whose interval has elapsed, and stops. Each task also keeps its own interval — this is the loop that drives them.') + '</p>' +
      C.switchRow(running ? 'Automatic runs are on' : 'Automatic runs are paused',
        running ? 'Due tasks update themselves on schedule.' : 'Nothing runs until you resume.',
        running, 'toggle-scheduler') +
      '<div class="mt16">' +
      C.field({ key: 's_tick', field: { type: 'number', label: 'Check interval (seconds)', default: s.general.schedulerTickSeconds, help: 'How often the scheduler looks for tasks that are due.' } }) +
      C.field({ key: 's_workers', field: { type: 'number', label: 'Concurrent reconciliations', default: s.general.schedulerWorkers, help: 'How many tasks may write to their provider at the same time.' } }) +
      C.switchField('s_runstart', !!s.general.runOnStart, 'Reconcile once at start-up', 'Run every enabled task the moment the daemon boots.') +
      '</div>' + saveBtn() + '</div>';
  }

  // Companion panel for the scheduler tab: explains the loop and shows the
  // values that are actually in force right now.
  function schedulerInfo(s, r) {
    var running = OZ.state.scheduler !== 'paused';
    function row(k, v) {
      return '<div class="between" style="padding:8px 0;border-bottom:1px solid var(--line-2)">' +
        '<span class="tiny muted">' + esc(k) + '</span><span class="mono" style="font-size:12.5px">' + esc(v) + '</span></div>';
    }
    return '<div class="vstack">' +
      '<div class="card pad"><div class="strong mb12">' + esc(t('How the schedule works')) + '</div>' +
      '<ol class="sched-list">' +
      '<li>' + t('The daemon wakes on the ticker, scans every task, and reconciles the ones whose own interval has elapsed.') + '</li>' +
      '<li>' + t('Each task keeps its individual interval — the ticker is just the heartbeat.') + '</li>' +
      '<li>' + t('A manual “Run all” reconciles everything immediately and leaves the schedule untouched.') + '</li>' +
      '</ol></div>' +
      '<div class="card pad"><div class="strong mb12">' + esc(t('Effective now')) + '</div>' +
      row(t('Status'), running ? t('Running') : t('Paused')) +
      row(t('Ticker'), t('every {n}s', { n: s.general.schedulerTickSeconds })) +
      row(t('Workers'), t('{n} at a time', { n: s.general.schedulerWorkers })) +
      row(t('Run at start-up'), s.general.runOnStart ? t('Yes') : t('No')) +
      row(t('History kept'), t('{n} day(s)', { n: s.general.runRetentionDays })) +
      '</div></div>';
  }

  OZ.views.settings = function () {
    var tab = OZ.state.param || 'general', D = OZ.data, s = D.settings, r = D.runtime;
    var body;

    if (tab === 'general') {
      body = '<div class="grid cols-2"><div class="card pad"><div class="strong mb16">Preferences</div>' +
        langField(s.general.language) +
        themeField(s.general.theme) +
        C.field({ key: 's_tz', field: { type: 'text', label: 'Timezone', default: s.general.timezone } }) +
        C.field({ key: 's_int', field: { type: 'number', label: 'Default interval (minutes)', default: s.general.defaultInterval } }) +
        C.field({ key: 's_retention', field: { type: 'number', label: 'Execution history (days)', default: s.general.runRetentionDays,
          help: 'Runs older than this are deleted for real, once an hour, so the database cannot grow without bound. 1–365.' } }) +
        saveBtn() + '</div>' +
        '<div class="card pad"><div class="strong mb16">Server</div>' +
        C.field({ key: 's_listen', field: { type: 'text', label: 'Listen address', mono: true, default: r.listen } }) +
        '<div class="callout mb16">' + t('Loopback by default. {c} is only reachable from this host. Bind {zero} to expose the panel — pair it with authentication.', { c: '<code>127.0.0.1:9876</code>', zero: '<code>0.0.0.0</code>' }) + '</div>' +
        '<div class="switch-row"><div class="txt"><div class="t">Open to LAN / WAN</div><div class="d">Allow access from other hosts</div></div>' + C.toggle(s.security.allowWan, 'noop') + '</div>' +
        '</div></div>';
    } else if (tab === 'scheduler') {
      body = '<div class="grid cols-2">' + schedulerCard(s, r) + schedulerInfo(s, r) + '</div>';
    } else if (tab === 'security') {
      body = '<div class="grid cols-2"><div class="card pad"><div class="strong mb16">Administrator</div>' +
        C.field({ key: 's_user', field: { type: 'text', label: 'Username', default: s.security.username } }) +
        C.field({ key: 's_pass', field: { type: 'secret', label: 'New password', help: 'Leave blank to keep the current password.' } }) +
        C.field({ key: 's_pass2', field: { type: 'secret', label: 'Confirm password' } }) +
        saveBtn() + '</div>' +
        '<div class="vstack"><div class="card pad"><div class="strong mb12">Access</div>' +
        C.switchRow('Require login', 'Prompt for credentials at the panel.', true, 'noop') +
        C.switchRow('Serve over TLS', 'Use an HTTPS certificate for the panel.', s.security.tls, 'noop') +
        C.switchRow('Allow remote writes', 'Let non-local clients trigger reconciliations.', false, 'noop') + '</div>' +
        '<div class="card pad"><div class="between mb12"><span class="strong">API tokens</span>' + C.pill('mute', t('{n} active', { n: s.security.apiTokens })) + '</div>' +
        C.banner('info', 'Tokens authenticate the CLI & external callers', 'Issue a scoped token for scripts instead of reusing your password.') +
        '<button class="btn mt16" data-act="open-modal" data-modal="invite">' + ico('plus') + 'Create token</button></div></div></div>';
    } else if (tab === 'sources') {
      var rows = D.ipSources.map(function (src) {
        var st = D.ipSourceTypes.filter(function (x) { return x.id === src.type; })[0] || { icon: 'globe', label: src.type };
        var used = D.jobs.filter(function (j) { return j.ipSourceId === src.id; }).length;
        return '<tr><td class="row-title">' + esc(src.name) +
          '<span class="row-sub mono sub-target">' + esc(src.target) + '</span></td>' +
          '<td class="col-type"><span class="hstack src-type" style="gap:7px" title="' + esc(st.label) + '">' + ico(st.icon) +
          '<span class="lbl">' + esc(st.label) + '</span></span></td>' +
          '<td>' + C.pill(src.family === 'v6' ? 'violet' : 'info', src.family.toUpperCase()) + '</td>' +
          '<td class="mono tiny col-target">' + esc(src.target) + '</td>' +
          '<td class="num">' + used + '</td>' +
          '<td class="r"><div class="hstack" style="justify-content:flex-end;gap:6px">' +
          '<button class="btn sm" data-act="test-source" data-id="' + esc(src.id) + '" title="' + esc(t('Test')) + '">' + ico('refresh') +
          '<span class="lbl">' + esc(t('Test')) + '</span></button>' +
          '<button class="icon-btn" data-act="edit-source" data-id="' + esc(src.id) + '" title="' + esc(t('Edit')) + '" aria-label="' + esc(t('Edit')) + '">' + ico('pencil') + '</button>' +
          '<button class="icon-btn act-copy" data-act="copy" data-copy="' + esc(src.target) + '" title="' + esc(t('Copy target')) + '">' + ico('copy') + '</button>' +
          '<button class="icon-btn" data-act="delete-source" data-id="' + esc(src.id) + '" title="' + esc(t('Delete')) + '">' + ico('trash') + '</button>' +
          '</div></td></tr>';
      }).join('');
      body = C.banner('info', t('What an IP source is'),
          esc(t('An IP source answers one question for a task — what address should this record hold right now? OzDDNS resolves it over HTTP, from a network interface, a shell command, or a fixed value, then writes it to your provider. Sources are reusable: pick one from the dropdown on the task form, or create a new one right there.'))) +
        '<div class="card mt16"><div class="card-head"><span class="h">' + esc(t('IP sources')) + '</span><span class="spacer"></span>' +
        '<button class="btn sm primary" data-act="open-modal" data-modal="source-new">' + ico('plus') + esc(t('Add source')) + '</button></div>' +
        (rows ? '<div class="table-wrap"><table class="table src-table"><thead><tr><th>' + esc(t('Name')) + '</th><th class="col-type">' + esc(t('Type')) + '</th><th>' + esc(t('Family')) + '</th><th>' + esc(t('Target')) + '</th><th>' + esc(t('Used by')) + '</th><th class="r">' + esc(t('Actions')) + '</th></tr></thead><tbody>' + rows + '</tbody></table></div>'
          : C.empty('globe', t('No IP sources yet'), esc(t("Add one to tell OzDDNS where this host's address comes from, then point a task at it.")),
            '<button class="btn primary" data-act="open-modal" data-modal="source-new">' + ico('plus') + esc(t('Add source')) + '</button>')) +
        '</div>';
    } else if (tab === 'notify') {
      body = '<div class="card"><div class="card-head"><span class="h">Notification channels</span><span class="spacer"></span>' +
        '<button class="btn sm primary" data-act="open-modal" data-modal="notify-new">' + ico('plus') + 'Add channel</button></div>' +
        '<div class="card-body vstack">' + s.notify.webhooks.map(function (w) {
          return '<div class="card pad" style="box-shadow:none"><div class="between"><div class="hstack">' +
            '<span class="logo lg" style="color:var(--coral-700);background:var(--coral-050)">' + ico(w.kind === 'slack' ? 'slack' : 'plug') + '</span>' +
            '<div><div class="strong">' + esc(w.name) + '</div><div class="mono tiny muted">' + esc(w.target) + '</div></div></div>' +
            '<div class="hstack">' + w.on.map(function (o) { return C.pill('mute', o); }).join('') +
            '<button class="btn sm" data-act="test-webhook" data-id="' + esc(w.id) + '">Test</button>' + C.toggle(w.enabled, 'noop') + '</div></div></div>';
        }).join('') + '</div>' +
        '<div class="card-body" style="border-top:1px solid var(--line-2)">' + C.banner('info', 'Shoutrrr-style URLs', 'Paste a service URL (Slack, Discord, ntfy, Gotify, Telegram…) and OzDDNS routes events to it.') + '</div></div>';
    } else if (tab === 'storage') {
      body = '<div class="grid cols-2"><div class="card pad"><div class="strong mb16">Database</div>' +
        C.field({ key: 'st_driver', field: { type: 'select', label: 'Driver', options: ['sqlite', 'mysql (coming soon)'], default: 'sqlite' } }) +
        C.field({ key: 'st_path', field: { type: 'text', label: 'SQLite path', mono: true, default: r.dbPath } }) +
        saveBtn() + '</div>' +
        '<div class="vstack"><div class="card pad"><div class="strong mb12">Backup & restore</div>' +
        '<div class="hstack wrap"><button class="btn" data-act="save-mock">' + ico('download') + 'Export config</button>' +
        '<button class="btn" data-act="save-mock">' + ico('upload') + 'Import config</button>' +
        '<button class="btn" data-act="save-mock">' + ico('database') + 'Snapshot DB</button></div></div>' +
        '<div class="card pad" style="border-color:#E6C6BC"><div class="strong mb8" style="color:var(--rust)">Danger zone</div>' +
        '<p class="tiny muted mb12">Wipe all providers, tasks and history. This cannot be undone.</p>' +
        '<button class="btn danger" data-act="save-mock">' + ico('alert-triangle') + 'Reset all data</button></div></div></div>';
    } else {
      body = '<div class="grid cols-2"><div class="card pad">' +
        '<div class="hstack mb12">' + '<span class="logo lg" style="color:var(--coral-700);background:var(--coral-050)">' + ico('compass') + '</span>' +
        '<div><div class="strong" style="font-size:16px">OzDDNS</div><div class="tiny muted">Provider-centric dynamic DNS</div></div></div>' +
        kv('Version', r.version) + kv('Build', r.build) + kv('Go runtime', 'go1.24 · darwin/arm64') + kv('Database', r.dbDriver + ' · ' + r.dbPath) +
        '<div class="hstack mt16"><button class="btn" data-act="save-mock">' + ico('external-link') + 'Documentation</button>' +
        '<button class="btn" data-act="save-mock">' + ico('git-branch') + 'Check for updates</button></div></div>' +
        '<div class="card pad"><div class="strong mb12">Installed providers</div>' +
        '<div class="hstack wrap" style="gap:8px">' + D.providerTypes.map(function (t) { return '<span class="pill mute">' + esc(t.label) + '</span>'; }).join('') + '</div>' +
        '<div class="divider"></div><div class="tiny muted">Providers are compiled into the binary and register themselves at start-up. Adding a new one means adding a package and registering a factory — no schema change required.</div></div></div>';
    }
    function kv(k, v) { return '<div class="between" style="padding:7px 0;border-bottom:1px solid var(--line-2)"><span class="tiny muted">' + esc(k) + '</span><span class="mono" style="font-size:12.5px">' + esc(v) + '</span></div>'; }

    return '<div class="page-head"><div class="titles"><div class="eyebrow">Configuration</div>' +
      '<h1 class="page-title">Settings</h1><p class="page-sub">Runtime, security, sources and delivery for this OzDDNS instance.</p></div></div>' +
      '<div class="tabs mb16">' + TABS.map(function (t) {
        return '<button class="' + (tab === t[0] ? 'on' : '') + '" data-act="go" data-href="#/settings' + (t[0] === 'general' ? '' : '/' + t[0]) + '">' + t[1] + '</button>';
      }).join('') + '</div>' + body;
  };

  // Offline-prototype behaviour for the IP-source row actions. api.js overrides
  // these with the live implementation whenever a server is reachable.
  OZ.actions['edit-source'] = function (el) {
    var s = OZ.get.ipSource(el.getAttribute('data-id'));
    if (!s) return;
    OZ.form['ns_name'] = s.name;
    OZ.form['ns_type'] = s.type;
    OZ.form['ns_family'] = s.family;
    OZ.form['ns_target'] = s.target;
    OZ.set({ modal: { type: 'source', source: s } });
  };
  OZ.actions['save-source'] = function (el) {
    var id = el.getAttribute('data-id'), s = OZ.get.ipSource(id);
    if (s) {
      s.name = OZ.form['ns_name'] || s.name;
      s.type = OZ.form['ns_type'] || s.type;
      s.family = OZ.form['ns_family'] || s.family;
      s.target = OZ.form['ns_target'] || s.target;
    }
    OZ.clearSourceForm();
    OZ.set({ modal: null });
    OZ.toast('ok', OZ.t('IP source updated'), (s && s.name) || id);
  };
  OZ.actions['test-source'] = function (el) {
    var s = OZ.get.ipSource(el.getAttribute('data-id'));
    var ip = s && s.family === 'v6' ? '2001:db8::23' : '203.0.113.42';
    OZ.toast('ok', OZ.t('Resolved {ip}', { ip: ip }), OZ.t('This is the address the task would use.'));
  };
  OZ.actions['delete-source'] = function (el) {
    var id = el.getAttribute('data-id'), s = OZ.get.ipSource(id);
    OZ.set({ modal: { type: 'confirm', title: OZ.t('Delete IP source?'),
      body: OZ.t('"{name}" will be removed. Tasks that reference it must be reassigned.', { name: s ? s.name : id }),
      confirm: OZ.t('Delete source'), danger: true, onConfirm: function () {
        OZ.data.ipSources = (OZ.data.ipSources || []).filter(function (x) { return x.id !== id; });
        OZ.set({ modal: null });
        OZ.toast('ok', OZ.t('IP source deleted'), s ? s.name : id);
      } } });
  };
})();
