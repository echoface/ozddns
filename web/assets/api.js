/* ============================================================
   OzDDNS — live API bridge
   Loads /api/v1/bootstrap and maps the server payload onto the
   same shape the (offline) mock dataset uses, so every view works
   unchanged. When the server is unreachable, OZ.live stays false
   and the prototype falls back to its mock data.
   ============================================================ */
(function () {
  var OZ = window.OZ = window.OZ || {};
  var base = '/api/v1';
  var auth = null;
  try { auth = sessionStorage.getItem('ozddns.auth') || null; } catch (e) {}

  var api = OZ.api = { base: base, unauthorized: false, lastError: null };
  OZ.live = false;
  var t = function (k, v) { return OZ.t(k, v); };

  /* ---------------- auth ---------------- */
  api.setAuth = function (user, pass) {
    auth = 'Basic ' + btoa(user + ':' + pass);
    try { sessionStorage.setItem('ozddns.auth', auth); } catch (e) {}
  };
  api.clearAuth = function () {
    auth = null;
    try { sessionStorage.removeItem('ozddns.auth'); } catch (e) {}
  };
  api.hasAuth = function () { return !!auth; };

  function headers(json) {
    var h = { 'Accept': 'application/json' };
    if (auth) h['Authorization'] = auth;
    if (json) h['Content-Type'] = 'application/json';
    return h;
  }

  /* ---------------- request ---------------- */
  api.req = function (path, opts) {
    opts = opts || {};
    var init = { method: opts.method || 'GET', headers: headers(opts.body != null), credentials: 'same-origin' };
    if (opts.body != null) init.body = JSON.stringify(opts.body);
    return fetch(base + path, init).then(function (res) {
      if (res.status === 401) {
        api.unauthorized = true;
        OZ.live = false;
        throw { status: 401, message: 'authentication required' };
      }
      return res.text().then(function (txt) {
        var data = null;
        if (txt) { try { data = JSON.parse(txt); } catch (e) { data = txt; } }
        if (!res.ok) {
          throw { status: res.status, message: (data && data.error) || res.statusText || ('HTTP ' + res.status) };
        }
        return data;
      });
    });
  };
  api.get = function (p) { return api.req(p); };
  api.post = function (p, b) { return api.req(p, { method: 'POST', body: b }); };
  api.patch = function (p, b) { return api.req(p, { method: 'PATCH', body: b }); };
  api.del = function (p) { return api.req(p, { method: 'DELETE' }); };
  api.health = function () { return api.get('/status'); };

  /* ---------------- mapping ---------------- */
  function toMs(t) { return t ? Date.parse(t) : null; }
  function toMsPos(t) {
    if (!t) return null;
    var n = Date.parse(t);
    if (!n || n < 0) return null; // zero-time (0001-01-01) => "not scheduled"
    return n;
  }

  api.mapProviderTypes = function (list) {
    return (list || []).map(function (t) {
      return {
        id: t.id, label: t.label, color: t.color, tag: t.tag, desc: t.desc,
        supportsProxied: !!t.supportsProxied,
        fields: (t.fields || []).map(function (f) {
          return { key: f.key, label: f.label, type: f.type, required: f.required,
            help: f.help, default: f.default, options: f.options, mono: f.mono };
        })
      };
    });
  };

  function mapProvider(p) {
    return {
      id: p.id, name: p.name, type: p.type, enabled: p.enabled,
      status: p.status, statusText: p.statusText,
      zones: p.zones || [], records: p.records || 0, config: p.config || {},
      lastValidated: toMs(p.lastValidated), createdAt: toMs(p.createdAt), updatedAt: toMs(p.updatedAt)
    };
  }

  function mapJob(j) {
    var st = j.state || {};
    var last = st.lastStatus || '';
    return {
      id: j.id, name: j.name, providerId: j.providerId, ipSourceId: j.ipSourceId,
      targets: (j.targets || []).map(function (t) {
        return { domain: t.domain, type: t.type, ttl: t.ttl, proxied: !!t.proxied, comment: t.comment || '' };
      }),
      intervalMinutes: j.intervalMinutes, runOnChangeOnly: j.runOnChangeOnly, jitter: j.jitter, enabled: j.enabled,
      lastRun: toMs(st.lastRunAt), lastStatus: last || 'nochange',
      nextRun: j.enabled ? toMsPos(st.nextRunAt) : null,
      current: st.current || '', desired: st.desired || '',
      lastChanged: toMs(st.lastChangedAt),
      error: st.error || undefined,
      note: last === 'skipped' ? (st.error || 'No address available from the source yet.') : undefined
    };
  }

  function mapRun(r) {
    return {
      id: String(r.id), jobId: r.jobId, startedAt: toMs(r.startedAt), durationMs: r.durationMs,
      status: r.status, oldIp: r.oldIp || null, newIp: r.newIp || null,
      trigger: r.trigger, message: r.message || '', records: r.records || 0
    };
  }

  function mapSource(s) {
    return { id: s.id, name: s.name, type: s.type, family: s.family, target: s.target, enabled: s.enabled };
  }

  function mapIP(ipInfo) {
    ipInfo = ipInfo || {};
    var at = toMs(ipInfo.checkedAt);
    return { v4: ipInfo.v4 || '', v4Source: ipInfo.v4Source || '—', v4Checked: at,
      v6: ipInfo.v6 || '', v6Source: ipInfo.v6Source || '—', v6Checked: at };
  }

  function mapRuntime(st) {
    st = st || {};
    return { scheduler: st.scheduler, uptimeSeconds: st.uptimeSeconds, version: st.version, build: st.build,
      dbPath: st.dbPath, dbDriver: st.dbDriver, listen: st.listen };
  }

  function mapScheduler(sc) {
    sc = sc || {};
    return { running: sc.running !== false, tickSeconds: sc.tickSeconds || 5,
      workers: sc.workers || 4, runOnStart: sc.runOnStart !== false,
      defaultIntervalMinutes: sc.defaultIntervalMinutes || 5 };
  }

  function mapSettings(s) {
    s = s || {};
    return {
      general: { language: OZ.i18n.normalize(s.language) || 'en', theme: s.theme || 'light',
        defaultInterval: s.defaultInterval || 5, timezone: s.timezone || 'UTC',
        schedulerPaused: !!s.schedulerPaused, schedulerTickSeconds: s.schedulerTickSeconds || 5,
        schedulerWorkers: s.schedulerWorkers || 4, runOnStart: s.runOnStart !== false,
        runRetentionDays: s.runRetentionDays || 1 },
      security: { username: s.username || '', passwordSet: !!s.passwordSet,
        allowWan: !!s.allowWan, tls: !!s.tls, apiTokens: 0 },
      notify: { webhooks: (s.notifications || []).map(function (n) {
        return { id: n.id, kind: n.kind, name: n.name, target: n.target,
          enabled: n.enabled, on: n.on || [] };
      }) },
      raw: s
    };
  }

  api.mapActivity = function (runs, jobs) {
    var byId = {};
    (jobs || []).forEach(function (j) { byId[j.id] = j; });
    return (runs || []).slice(0, 12).map(function (r) {
      var j = byId[r.jobId];
      var kind = r.status === 'success' ? 'ok' : r.status === 'failed' ? 'err' : 'info';
      var label = OZ.t({ success: 'updated', nochange: 'in sync', failed: 'failed', skipped: 'skipped' }[r.status] || r.status);
      var text = r.message || '';
      if (r.oldIp && r.newIp && r.oldIp !== r.newIp) text = r.oldIp + ' → ' + r.newIp + (text ? ' · ' + text : '');
      return { at: r.startedAt, kind: kind, title: (j ? j.name : r.jobId) + ' ' + label, text: text };
    });
  };

  /* ---------------- bootstrap ---------------- */
  api.load = function () {
    return api.get('/bootstrap').then(function (b) {
      applyBootstrap(b);
      OZ.live = true;
      api.unauthorized = false;
      api.lastError = null;
      return b;
    }).catch(function (err) {
      api.lastError = err && err.message;
      throw err;
    });
  };

  function applyBootstrap(b) {
    var D = OZ.data || (OZ.data = {});
    D.providerTypes = api.mapProviderTypes(b.providerTypes);
    D.ipSourceTypes = (b.ipSourceTypes || []).slice();
    D.ipSources = (b.ipSources || []).map(mapSource);
    // Local NICs + their addresses, for the task wizard's interface picker.
    D.interfaces = (b.interfaces || []).slice();
    D.providers = (b.providers || []).map(mapProvider);
    D.jobs = (b.jobs || []).map(mapJob);
    D.runs = (b.runs || []).map(mapRun);
    D.ipInfo = mapIP(b.ip);
    D.runtime = mapRuntime(b.status);
    D.scheduler = mapScheduler(b.scheduler);
    D.settings = mapSettings(b.settings);
    D.activity = api.mapActivity(D.runs, D.jobs);
    OZ.state.scheduler = (b.status && b.status.scheduler) || (D.scheduler.running ? 'running' : 'paused');
    // The first real paint happens now — release the boot skeleton.
    OZ.state.boot = 'ready';
    // The stored language is the source of truth once we have a server —
    // unless the caller pinned one explicitly with ?lang=.
    if (!OZ.i18n.override) OZ.i18n.set(D.settings.general.language, true);
    // Same for the theme — a ?theme= keeps its own override for the session.
    if (!OZ.theme.override) OZ.theme.set(D.settings.general.theme, false);
  }

  api.refresh = function (quiet) {
    return api.load().then(function () { OZ.notify(); }).catch(function (err) {
      if (!err || err.status !== 401) OZ.toast('err', t('Refresh failed'), api.lastError || t('The server did not respond.'));
    });
  };

  /* ---------------- live action wiring ---------------- */
  var mock = {};
  function wrap(name, impl) {
    mock[name] = OZ.actions[name];
    OZ.actions[name] = function (el, e) {
      // Only the truly offline prototype (server unreachable) falls back to
      // the mock handlers. A reachable-but-unauthenticated server still uses
      // the live path, so the sign-in screen can actually authenticate.
      if (OZ.state.boot === 'offline') return mock[name] ? mock[name](el, e) : undefined;
      try { return impl(el, e); } catch (err) { return failToast(err); }
    };
  }

  function failToast(err, title) {
    var msg = (err && (err.message || err.error)) || String(err || 'unknown error');
    if (err && err.status === 401) { OZ.navigate('#/login'); OZ.toast('err', t('Sign-in required'), t('Enter your credentials to continue.')); return; }
    OZ.toast('err', title || t('Something went wrong'), msg, 5200);
  }

  wrap('toggle-scheduler', function () {
    var next = OZ.state.scheduler !== 'running';
    return api.post('/scheduler', { running: next }).then(function (res) {
      OZ.state.scheduler = res.running ? 'running' : 'paused';
      OZ.toast('ok', t(res.running ? 'Scheduler resumed' : 'Scheduler paused'),
        t(res.running ? 'Jobs will run on schedule.' : 'No automatic runs until resumed.'));
      OZ.notify();
    }).catch(failToast);
  });

  // A one-click theme flip should stick, so it is written back to the server
  // (the picker in Settings persists via the normal "Save changes" path).
  wrap('toggle-theme', function () {
    var next = OZ.theme.isDark() ? 'light' : 'dark';
    OZ.theme.set(next, true); // instant: repaint before the round-trip
    return api.patch('/settings', { theme: next }).then(function () {
      if (OZ.data.settings && OZ.data.settings.general) OZ.data.settings.general.theme = next;
    }).catch(function (err) { failToast(err, t('Could not save the theme')); });
  });

  wrap('run-job', function (el) {
    var j = OZ.get.job(el.getAttribute('data-id'));
    OZ.toast('info', t('Running {name}…', { name: (j ? j.name : 'task') }), t('Reconciling records against the provider.'), 2600);
    return api.post('/jobs/' + (j ? j.id : el.getAttribute('data-id')) + '/run').then(function () {
      return api.refresh();
    }).then(function () {
      if (!j) return;
      var latest = OZ.get.job(j.id);
      var st = latest ? latest.lastStatus : 'nochange';
      if (st === 'failed') OZ.toast('err', t('{name} failed', { name: j.name }), (latest && latest.error) || t('Update failed'));
      else if (st === 'success') OZ.toast('ok', t('{name} · record updated', { name: j.name }), t('The provider accepted the new address.'));
      else if (st === 'skipped') OZ.toast('info', t('{name} · skipped', { name: j.name }), (latest && latest.note) || t('No address available.'));
      else OZ.toast('ok', t('{name} · in sync', { name: j.name }), t('Records already match the desired address.'));
      OZ.notify();
    }).catch(function (err) { failToast(err, t('Run failed')); return api.refresh(true); });
  });

  // Refresh the *public address* only (Overview's IP card) — deliberately not
  // the same as "Run all", which reconciles tasks.
  wrap('refresh-ip', function () {
    return api.get('/ip').then(function (ip) {
      OZ.data.ipInfo = mapIP(ip);
      OZ.toast('ok', t('IP refreshed'), t('Re-checked the public address.'));
      OZ.notify();
    }).catch(function (err) { failToast(err, t('Refresh failed')); });
  });

  wrap('run-all', function () {
    // One server-side call reconciles every enabled task sequentially and
    // reports the aggregate — the periodic schedule stays the scheduler's job.
    var n = (OZ.data.jobs || []).filter(function (j) { return j.enabled; }).length;
    if (!n) { OZ.toast('info', t('Nothing to run'), t('No enabled tasks.')); return; }
    OZ.toast('info', t('Reconciling {n} task(s)…', { n: n }), t('Reading records and diffing.'), 2600);
    return api.post('/jobs/run-all').then(function (sum) {
      return api.refresh(true).then(function () {
        sum = sum || {};
        var failed = sum.failed || 0;
        OZ.toast(failed ? 'err' : 'ok', failed ? t('Some tasks failed') : t('All tasks reconciled'),
          t('{success} updated · {nochange} unchanged · {skipped} skipped · {failed} failed',
            { success: sum.success || 0, nochange: sum.noChange || 0, skipped: sum.skipped || 0, failed: failed }));
        OZ.notify();
      });
    }).catch(failToast);
  });

  wrap('toggle-job', function (el) {
    var j = OZ.get.job(el.getAttribute('data-id'));
    return api.patch('/jobs/' + j.id, { enabled: !j.enabled }).then(function () {
      OZ.toast(!j.enabled ? 'ok' : 'info', t(!j.enabled ? 'Enabled {name}' : 'Paused {name}', { name: j.name }));
      return api.refresh(true);
    }).catch(failToast);
  });

  wrap('toggle-provider', function (el) {
    var p = OZ.get.provider(el.getAttribute('data-id'));
    return api.patch('/providers/' + p.id, { enabled: !p.enabled }).then(function () {
      OZ.toast(!p.enabled ? 'ok' : 'info', t(!p.enabled ? 'Enabled {name}' : 'Disabled {name}', { name: p.name }));
      return api.refresh(true);
    }).catch(failToast);
  });

  wrap('validate-provider', function (el) {
    var p = OZ.get.provider(el.getAttribute('data-id'));
    OZ.toast('info', t('Validating {name}…', { name: p.name }), t('Checking credentials & zones.'), 2000);
    return api.post('/providers/' + p.id + '/validate').then(function (res) {
      if (res.ok) OZ.toast('ok', t('{name} is valid', { name: p.name }), t('Credentials and zones look good.'));
      else OZ.toast('err', t('{name} failed validation', { name: p.name }), res.error || t('The provider rejected the request.'), 5200);
      return api.refresh(true);
    }).catch(function (err) { failToast(err, t('Validation failed')); return api.refresh(true); });
  });

  wrap('delete-provider', function (el) {
    var p = OZ.get.provider(el.getAttribute('data-id'));
    OZ.set({ modal: { type: 'confirm', title: t('Delete provider?'),
      body: t('"{name}" will be removed. Jobs that reference it must be reassigned.', { name: p.name }),
      confirm: t('Delete provider'), danger: true, onConfirm: function () {
        api.del('/providers/' + p.id).then(function () {
          OZ.toast('ok', t('Provider deleted'), p.name);
          OZ.navigate('#/providers');
          return api.refresh(true);
        }).catch(function (err) { failToast(err, t('Delete failed')); });
      } } });
  });

  wrap('delete-job', function (el) {
    var j = OZ.get.job(el.getAttribute('data-id'));
    OZ.set({ modal: { type: 'confirm', title: t('Delete sync task?'),
      body: t('"{name}" and its history will be removed.', { name: j.name }), confirm: t('Delete task'), danger: true,
      onConfirm: function () {
        api.del('/jobs/' + j.id).then(function () {
          OZ.toast('ok', t('Task deleted'), j.name);
          OZ.navigate('#/jobs');
          return api.refresh(true);
        }).catch(function (err) { failToast(err, t('Delete failed')); });
      } } });
  });

  wrap('test-webhook', function (el) {
    var id = el && el.getAttribute('data-id');
    OZ.toast('info', t('Sending test notification…'), t('Delivering a sample payload.'), 1800);
    return api.post('/settings/test-notify', { id: id || '' }).then(function (res) {
      if (res.ok) OZ.toast('ok', t('Test delivered'), t('The endpoint responded successfully.'));
      else OZ.toast('err', t('Delivery failed'), res.error || t('The endpoint rejected the request.'), 5200);
    }).catch(function (err) { failToast(err, t('Delivery failed')); });
  });

  wrap('save-provider', function (el) {
    var p = OZ.get.provider(el.getAttribute('data-id'));
    var t = OZ.get.providerType(p.type) || { fields: [] };
    var cfg = {};
    (t.fields || []).forEach(function (f) {
      var v = fieldVal(p.id + '_' + f.key);
      if (v !== undefined && v !== '') cfg[f.key] = coerce(v);
    });
    return api.patch('/providers/' + p.id, { name: p.name, config: cfg }).then(function () {
      OZ.toast('ok', OZ.t('Saved'), OZ.t('{name} configuration updated.', { name: p.name }));
      return api.refresh(true);
    }).catch(function (err) { failToast(err, OZ.t('Save failed')); });
  });

  wrap('save-settings', function () {
    var payload = {
      language: fieldVal('s_lang'), theme: fieldVal('s_theme'), timezone: fieldVal('s_tz'),
      defaultInterval: toInt(fieldVal('s_int')),
      schedulerTickSeconds: toInt(fieldVal('s_tick')),
      schedulerWorkers: toInt(fieldVal('s_workers')),
      runRetentionDays: toInt(fieldVal('s_retention')),
      runOnStart: boolOrUndef(fieldVal('s_runstart')),
      listen: fieldVal('s_listen'), username: fieldVal('s_user'),
      notifications: (OZ.data.settings.notify.webhooks || []).slice()
    };
    if (fieldVal('s_pass')) payload.password = fieldVal('s_pass');
    return api.patch('/settings', prune(payload)).then(function () {
      if (payload.language) OZ.i18n.set(payload.language, true);
      if (payload.theme) OZ.theme.set(payload.theme, true);
      OZ.toast('ok', t('Saved'), t('Settings persisted.'));
      return api.refresh(true);
    }).catch(function (err) { failToast(err, t('Save failed')); });
  });

  wrap('add-source', function () {
    var name = fieldVal('ns_name');
    return api.post('/ip-sources', {
      name: name, type: fieldVal('ns_type'), family: fieldVal('ns_family'), target: fieldVal('ns_target')
    }).then(function () {
      OZ.clearSourceForm();
      OZ.set({ modal: null });
      OZ.toast('ok', t('IP source added'), name || '');
      return api.refresh(true);
    }).catch(function (err) { failToast(err, t('Could not add source')); });
  });

  // Edit a stored source in place. Every task that reuses it picks the change up
  // on its next run, which the sheet spells out before you save.
  wrap('edit-source', function (el) {
    var s = OZ.get.ipSource(el.getAttribute('data-id'));
    if (!s) return;
    OZ.form['ns_name'] = s.name;
    OZ.form['ns_type'] = s.type;
    OZ.form['ns_family'] = s.family;
    OZ.form['ns_target'] = s.target;
    OZ.set({ modal: { type: 'source', source: s } });
  });

  wrap('save-source', function (el) {
    var id = el.getAttribute('data-id'), name = fieldVal('ns_name');
    return api.patch('/ip-sources/' + id, {
      name: name, type: fieldVal('ns_type'), family: fieldVal('ns_family'), target: fieldVal('ns_target')
    }).then(function () {
      OZ.clearSourceForm();
      OZ.set({ modal: null });
      OZ.toast('ok', t('IP source updated'), name || '');
      return api.refresh(true);
    }).catch(function (err) { failToast(err, t('Could not update source')); });
  });

  // Resolve an address from a stored source right now — proves the source works
  // without touching the provider.
  wrap('test-source', function (el) {
    var id = el.getAttribute('data-id');
    OZ.toast('info', t('Resolving…'), t('Reading the address from this source.'), 1800);
    return api.post('/ip-sources/' + id + '/test').then(function (res) {
      res = res || {};
      if (res.ok) OZ.toast('ok', t('Resolved {ip}', { ip: res.ip }), t('This is the address the task would use.'));
      else OZ.toast('err', t('Could not resolve an address'), res.error || t('The source returned no usable address.'), 5200);
    }).catch(function (err) { failToast(err, t('Resolve failed')); });
  });

  wrap('delete-source', function (el) {
    var id = el.getAttribute('data-id'), s = OZ.get.ipSource(id);
    var name = s ? s.name : id;
    OZ.set({ modal: { type: 'confirm', title: t('Delete IP source?'),
      body: t('"{name}" will be removed. Tasks that reference it must be reassigned.', { name: name }),
      confirm: t('Delete source'), danger: true, onConfirm: function () {
        api.del('/ip-sources/' + id).then(function () {
          OZ.toast('ok', t('IP source deleted'), name);
          return api.refresh(true);
        }).catch(function (err) { failToast(err, t('Delete failed')); });
      } } });
  });

  wrap('add-notify', function () {
    var s = OZ.data.settings.raw || {};
    var list = (s.notifications || []).slice();
    var name = fieldVal('nn_name') || 'Webhook';
    list.push({ id: 'n-' + Date.now().toString(36), kind: fieldVal('nn_kind') || 'webhook',
      name: name, target: fieldVal('nn_target') || '', enabled: true,
      on: [(fieldVal('nn_on') || 'failure')] });
    return api.patch('/settings', prune({ notifications: list })).then(function () {
      OZ.set({ modal: null });
      OZ.toast('ok', t('Channel added'), name);
      return api.refresh(true);
    }).catch(function (err) { failToast(err, t('Could not add channel')); });
  });

  wrap('pw-create', function () {
    var t = OZ.get.providerType(OZ.state.modal.typeId);
    var cfg = {};
    (t.fields || []).forEach(function (f) {
      var v = fieldVal('pw_' + f.key);
      if (v !== undefined && v !== '') cfg[f.key] = coerce(v);
    });
    return api.post('/providers', { name: fieldVal('pw_name') || t.label, type: t.id, config: cfg }).then(function () {
      OZ.set({ modal: null });
      OZ.toast('ok', OZ.t('Provider added'), OZ.t('{label} validated and ready.', { label: t.label }));
      return api.refresh(true);
    }).catch(function (err) { failToast(err, OZ.t('Could not add provider')); });
  });

  wrap('wiz-create', function () {
    var w = OZ.wizardStep ? OZ.wizardStep() : null;
    if (!w) return;
    var providerId = fieldVal('wiz_provider') || w.providerId;
    if (!providerId) { OZ.toast('err', t('Pick a DNS provider'), t('Choose where these records should be written.')); return; }
    var domains = OZ.splitDomains(fieldVal('wiz_domains'));
    if (!domains.length) { OZ.toast('err', t('Add at least one domain'), t('Enter one domain per line.')); return; }
    // A stored, reused source decides the address family; a new one follows the
    // family chosen on the form.
    var pick = fieldVal('wiz_src_pick');
    var shared = (pick && pick !== '__new__') ? OZ.get.ipSource(pick) : null;
    var family = shared ? (shared.family === 'v6' ? 'v6' : 'v4') : (w.family === 'v6' ? 'v6' : 'v4');
    var type = family === 'v6' ? 'AAAA' : 'A';
    var name = fieldVal('wiz_name') || domains[0];
    var targets = domains.map(function (d) {
      return { domain: d, type: type, ttl: 300, proxied: false, comment: '' };
    });
    var srcPromise = shared ? Promise.resolve({ id: shared.id }) : api.post('/ip-sources', {
      name: name + ' source', type: w.sourceType, family: family, target: sourceTarget(w)
    });
    var editId = w.editId || null;
    var payload = {
        name: name, providerId: providerId,
        targets: targets,
        intervalMinutes: parseInt(fieldVal('wiz_interval') || w.interval, 10) || 5,
        runOnChangeOnly: w.runOnChangeOnly, jitter: w.jitter, enabled: true
    };
    return srcPromise.then(function (src) {
      payload.ipSourceId = src.id;
      // Same payload either way: creating posts a new task, editing patches the
      // one being edited (the source may itself be brand new).
      return editId ? api.patch('/jobs/' + editId, payload) : api.post('/jobs', payload);
    }).then(function () {
      OZ.state.wizard = null;
      OZ.toast('ok', t(editId ? 'Task saved' : 'Task created'), t('It will run on the next scheduler tick.'));
      OZ.navigate(editId ? '#/jobs/' + editId : '#/jobs');
      return api.refresh(true);
    }).catch(function (err) { failToast(err, t(editId ? 'Could not save task' : 'Could not create task')); });
  });

  wrap('login', function () {
    var user = OZ.form['login_user'] || 'admin';
    var pass = OZ.form['login_pass'] || '';
    if (!pass) { OZ.set({ authPasswordError: true }); return; }
    api.setAuth(user, pass);
    return api.health().then(function () {
      OZ.set({ authPasswordError: false });
      OZ.toast('ok', t('Welcome back'), t('Signed in as {user}', { user: user }));
      return api.load();
    }).then(function () {
      OZ.navigate('#/overview');
      OZ.notify();
    }).catch(function (err) {
      api.clearAuth();
      OZ.set({ authPasswordError: true });
      OZ.toast('err', t('Sign-in failed'), (err && err.message) || t('Check your credentials.'), 4200);
    });
  });

  function sourceTarget(w) {
    if (w.sourceType === 'http') return fieldVal('wiz_src_url') || w.sourceValue || 'https://api.ipify.org';
    if (w.sourceType === 'interface') return fieldVal('wiz_src_if') || 'en0';
    if (w.sourceType === 'command') return fieldVal('wiz_src_cmd') || 'tailscale ip -6';
    return fieldVal('wiz_src_static') || '';
  }
  // fieldVal reads the live DOM input (falling back to the captured form map).
  function fieldVal(key) {
    var el = document.querySelector('[data-field="' + key + '"]');
    if (!el) return OZ.form[key];
    if (el.getAttribute && el.getAttribute('data-type') === 'bool') return el.classList.contains('on');
    if (el.type === 'checkbox') return el.checked;
    return el.value;
  }
  function toInt(v) {
    if (v == null || v === '') return undefined;
    var n = parseInt(v, 10);
    return isNaN(n) ? undefined : n;
  }
  function boolOrUndef(v) {
    if (v === true || v === false) return v;
    if (v === 'true') return true;
    if (v === 'false') return false;
    return undefined;
  }
  function coerce(v) {
    if (v === 'true') return true;
    if (v === 'false') return false;
    if (v !== '' && !isNaN(v)) return Number(v);
    return v;
  }
  function prune(o) {
    var out = {};
    for (var k in o) { if (o[k] !== undefined && o[k] !== null && o[k] !== '') out[k] = o[k]; }
    return out;
  }

  /* ---------------- boot ---------------- */
  api.boot = function () {
    return api.load().then(function () {
      OZ.live = true;
      OZ.notify();
    }).catch(function (err) {
      if (err && err.status === 401) {
        // Sign-in required: show the login screen (it needs no dataset).
        OZ.state.boot = 'ready';
        if (!/^#\/login/.test(window.location.hash)) OZ.navigate('#/login');
        OZ.notify();
        return;
      }
      // Server unreachable: fall back to the bundled offline prototype rather
      // than leaving the skeleton spinning forever.
      OZ.state.boot = 'offline';
      OZ.notify();
    });
  };

  OZ.api._internal = { mapProvider: mapProvider, mapJob: mapJob, mapRun: mapRun, mapSource: mapSource,
    mapSettings: mapSettings, mapRuntime: mapRuntime, mapIP: mapIP };

  api.boot();
})();
