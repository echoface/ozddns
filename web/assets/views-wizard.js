/* OzDDNS — new sync task.
   A single-page form, ddns-go style: pick the address family, the provider,
   list the domains, and choose where the address comes from. Everything else
   (interval, write mode, ...) hides behind "Advanced options", so the common
   case is four fields. */
(function () {
  var OZ = window.OZ, ico = window.ico, U = OZ.util, esc = U.esc, C = OZ.c;
  var t = function (k, v) { return OZ.t(k, v); };

  // The record type is implied by the address family, never chosen freely:
  // IPv4 sources write A records, IPv6 sources write AAAA records.
  function recordType(family) { return family === 'v6' ? 'AAAA' : 'A'; }

  // Domains may be separated by newlines, commas or semicolons.
  function splitDomains(s) {
    return String(s == null ? '' : s).split(/[\n,;]+/).map(function (x) { return x.trim(); }).filter(Boolean);
  }
  OZ.splitDomains = splitDomains;

  function blank() {
    return {
      editId: null, name: '', domains: '', family: 'v4', providerId: null, sourceType: 'http',
      sourcePickId: null,
      interval: 5, runOnChangeOnly: true, jitter: true, runOnStart: false, advanced: false
    };
  }

  // The form owns its state so a re-render (theme toggle, live refresh, …) never
  // drops what was typed. `wantId` is the task being edited, or null for a new
  // one; switching between the two starts a clean form, and editing pre-fills
  // every field from the stored task.
  function ensure(wantId) {
    var want = wantId || null, w = OZ.state.wizard;
    if (w && w.editId === want) return w;
    w = blank();
    w.editId = want;
    Object.keys(OZ.form).forEach(function (k) { if (k.indexOf('wiz_') === 0) delete OZ.form[k]; });
    if (want) {
      var j = OZ.get.job(want), src = j ? OZ.get.ipSource(j.ipSourceId) : null;
      if (j) {
        var first = (j.targets || [])[0] || {};
        w.name = j.name;
        w.providerId = j.providerId;
        w.interval = j.intervalMinutes || 5;
        w.runOnChangeOnly = j.runOnChangeOnly !== false;
        w.jitter = !!j.jitter;
        w.domains = (j.targets || []).map(function (x) { return x.domain; }).join('\n');
        // A stored task always points at a stored source, so the form opens in
        // "reuse" mode — switching to "create a new source" is an explicit act.
        w.sourcePickId = src ? src.id : null;
        w.family = src ? (src.family === 'v6' ? 'v6' : 'v4') : (first.type === 'AAAA' ? 'v6' : 'v4');
        OZ.form.wiz_name = j.name;
        OZ.form.wiz_domains = w.domains;
        OZ.form.wiz_interval = String(w.interval);
      }
    }
    OZ.state.wizard = w;
    return w;
  }
  // Used by the row of actions and by api.js; keeps whatever is on screen.
  OZ.wizardStep = function () { return OZ.state.wizard || ensure(null); };
  OZ.wizardFor = ensure;
  var init = OZ.wizardStep;

  /* ---- actions ---- */
  OZ.actions['wiz-family'] = function (el) { init().family = el.getAttribute('data-id') === 'v6' ? 'v6' : 'v4'; OZ.notify(); };
  OZ.actions['wiz-provider'] = function (el) { init().providerId = el.value || null; OZ.notify(); };
  OZ.actions['wiz-src-type'] = function (el) { init().sourceType = el.value; OZ.notify(); };
  // "" / "__new__" means "build a fresh source"; any other value is an id of a
  // stored source this task should share.
  OZ.actions['wiz-src-pick'] = function (el) {
    var w = init(), v = el.value || '';
    w.sourcePickId = (v && v !== '__new__') ? v : null;
    var s = w.sourcePickId ? OZ.get.ipSource(w.sourcePickId) : null;
    // Reusing a source also fixes the address family (and therefore the record
    // type) — the two can never contradict each other.
    if (s && s.family) w.family = s.family === 'v6' ? 'v6' : 'v4';
    OZ.notify();
  };
  OZ.actions['wiz-write-mode'] = function (el) { init().runOnChangeOnly = el.getAttribute('data-id') === 'change'; OZ.notify(); };
  OZ.actions['wiz-jitter'] = function () { var w = init(); w.jitter = !w.jitter; OZ.notify(); };
  OZ.actions['wiz-runstart'] = function () { var w = init(); w.runOnStart = !w.runOnStart; OZ.notify(); };
  OZ.actions['wiz-advanced'] = function () { var w = init(); w.advanced = !w.advanced; OZ.notify(); };
  OZ.actions['wiz-cancel'] = function () {
    var w = OZ.state.wizard, to = w && w.editId ? '#/jobs/' + w.editId : '#/jobs';
    OZ.state.wizard = null;
    OZ.navigate(to);
  };
  // Offline-prototype create; api.js overrides it with the live implementation.
  OZ.actions['wiz-create'] = function () {
    var w = OZ.wizardStep(), to = w.editId ? '#/jobs/' + w.editId : '#/jobs';
    OZ.toast('ok', w.editId ? t('Task saved') : t('Task created'), t('It will run on the next scheduler tick.'));
    OZ.state.wizard = null; OZ.navigate(to);
  };

  // Reads a captured form value, falling back to a default.
  function val(key, dflt) { var v = OZ.form[key]; return (v == null || v === '') ? (dflt || '') : v; }

  // Interfaces + their addresses for the chosen family (ddns-go style picker).
  function ifaceOptions(family) {
    var out = [];
    (OZ.data.interfaces || []).forEach(function (ifc) {
      (ifc.addresses || []).forEach(function (a) {
        if (a.family !== family) return;
        out.push({ value: a.target, label: ifc.name + ' · ' + a.addr + ' /' + a.prefix });
      });
    });
    return out;
  }

  // The value control that pairs with the chosen IP-source kind.
  function sourceValue(w) {
    if (w.sourceType === 'interface') {
      var opts = ifaceOptions(w.family);
      if (!opts.length) {
        return '<input class="input mono" data-field="wiz_src_if" value="' + esc(val('wiz_src_if', 'en0')) + '" placeholder="en0">';
      }
      var cur = val('wiz_src_if');
      if (!opts.some(function (o) { return o.value === cur; })) cur = opts[0].value;
      return '<select class="input mono" data-field="wiz_src_if">' + opts.map(function (o) {
        return '<option value="' + esc(o.value) + '"' + (o.value === cur ? ' selected' : '') + '>' + esc(o.label) + '</option>';
      }).join('') + '</select>';
    }
    if (w.sourceType === 'command') {
      return '<input class="input mono" data-field="wiz_src_cmd" value="' + esc(val('wiz_src_cmd')) + '" placeholder="tailscale ip -6">';
    }
    if (w.sourceType === 'static') {
      return '<input class="input mono" data-field="wiz_src_static" value="' + esc(val('wiz_src_static')) + '" placeholder="' +
        esc(w.family === 'v6' ? '2001:db8::1' : '203.0.113.42') + '">';
    }
    return '<input class="input mono" data-field="wiz_src_url" value="' + esc(val('wiz_src_url')) + '" placeholder="https://api.ipify.org">';
  }

  function sourceHint(w) {
    if (w.sourceType === 'interface') return t('Pick which interface and address the records should follow.');
    if (w.sourceType === 'command') return t('The first matching address is extracted from stdout.');
    if (w.sourceType === 'static') return t('Pin a fixed address (testing or fixed-line).');
    return t('Returns the caller IP as plain text.');
  }

  // A provider option label that stays unambiguous when names repeat.
  function providerLabel(p, dupes) {
    var pt = OZ.get.providerType(p.type) || { label: p.type };
    var base = p.name;
    if (String(p.name).toLowerCase() !== String(pt.label).toLowerCase()) base += ' · ' + pt.label;
    if (dupes[p.name] > 1) base += ' (' + String(p.id).slice(-5) + ')';
    return base;
  }

  OZ.views.jobNew = function (editId) {
    var w = ensure(editId), D = OZ.data, editing = !!w.editId;
    var type = recordType(w.family);

    var savedSources = (D.ipSources || []).filter(function (s) { return s.enabled !== false; });
    var picked = w.sourcePickId ? (savedSources.filter(function (s) { return s.id === w.sourcePickId; })[0] || OZ.get.ipSource(w.sourcePickId)) : null;
    if (picked) w.family = picked.family === 'v6' ? 'v6' : 'v4';
    type = recordType(w.family);

    var provs = (D.providers || []).filter(function (p) { return p.enabled; });
    var dupes = {};
    provs.forEach(function (p) { dupes[p.name] = (dupes[p.name] || 0) + 1; });
    var providerField = provs.length
      ? '<div class="field"><label>' + esc(t('DNS provider')) + ' <span class="req">*</span></label>' +
        '<select class="input" data-field="wiz_provider" data-act="wiz-provider">' +
        '<option value=""' + (w.providerId ? '' : ' selected') + ' disabled>' + esc(t('Choose a provider…')) + '</option>' +
        provs.map(function (p) {
          return '<option value="' + esc(p.id) + '"' + (w.providerId === p.id ? ' selected' : '') + '>' +
            esc(providerLabel(p, dupes)) + '</option>';
        }).join('') + '</select></div>'
      : '<div class="field"><label>' + esc(t('DNS provider')) + '</label>' +
        C.banner('warn', t('No providers yet'), esc(t('Add a DNS provider first — right now OzDDNS has nowhere to write this record.'))) +
        '<div class="mt8"><button class="btn sm" data-act="go" data-href="#/providers">' + ico('plus') + esc(t('Add a provider')) + '</button></div></div>';

    var domains = val('wiz_domains');
    var nDomains = splitDomains(domains).length;
    var domainField = '<div class="field"><label>' + esc(t('Domains')) + ' <span class="req">*</span></label>' +
      '<textarea class="input mono" rows="4" data-field="wiz_domains" placeholder="' + esc(t('One domain per line, e.g.') + '\nhome.example.com\nnas.example.com') + '">' + esc(domains) + '</textarea>' +
      '<span class="help">' + esc(t('One domain per line. Each becomes a {type} record.', { type: type })) +
      (nDomains ? ' · ' + esc(t('{n} record(s)', { n: nDomains })) : '') + '</span></div>';

    var advanced = '<button type="button" class="btn sm mt16" data-act="wiz-advanced">' +
      ico(w.advanced ? 'chevron-down' : 'chevron-right') + esc(t('Advanced options')) + '</button>' +
      (w.advanced ? '<div class="divider"></div>' +
        C.field({ key: 'wiz_interval', field: { key: 'wiz_interval', type: 'number', label: 'Interval (minutes)', default: w.interval } }) +
        '<div class="field"><label>' + esc(t('Write mode')) + '</label><div class="seg" style="width:100%">' +
        '<button type="button" class="' + (w.runOnChangeOnly ? 'on' : '') + '" style="flex:1" data-act="wiz-write-mode" data-id="change">' + esc(t('Only on change')) + '</button>' +
        '<button type="button" class="' + (w.runOnChangeOnly ? '' : 'on') + '" style="flex:1" data-act="wiz-write-mode" data-id="always">' + esc(t('Always rewrite')) + '</button></div>' +
        '<span class="help">' + esc(t('Change-only avoids needless API calls and keeps records stable.')) + '</span></div>' +
        C.switchRow(t('Run once on start'), t('Reconcile immediately when OzDDNS boots.'), !!w.runOnStart, 'wiz-runstart') +
        C.switchRow(t('Add jitter'), t('Sprinkle runs ±10% to avoid thundering herds.'), !!w.jitter, 'wiz-jitter') : '');

    var body =
      '<div class="field"><label>' + esc(t('Task name')) + '</label>' +
      '<input class="input" data-field="wiz_name" placeholder="' + esc(t('e.g. Home IPv4')) + '" value="' + esc(val('wiz_name')) + '">' +
      '<span class="help">' + esc(t('Optional. Defaults to the first domain.')) + '</span></div>' +
      '<div class="row2">' +
      '<div class="field"><label>' + esc(t('IP version')) + '</label>' +
      '<div class="seg" style="width:100%"><button type="button" class="' + (w.family === 'v4' ? 'on' : '') + '" style="flex:1"' + (picked ? ' disabled' : '') + ' data-act="wiz-family" data-id="v4">IPv4 · A</button>' +
      '<button type="button" class="' + (w.family === 'v6' ? 'on' : '') + '" style="flex:1"' + (picked ? ' disabled' : '') + ' data-act="wiz-family" data-id="v6">IPv6 · AAAA</button></div>' +
      '<span class="help">' + esc(t(picked ? 'Set by the IP source family.' : 'Sets the record type for every domain below.')) + '</span></div>' +
      providerField + '</div>' +
      sourceField(w, savedSources, picked) +
      domainField +
      advanced;

    var foot = '<div class="hstack between mt24">' +
      '<button class="btn" data-act="wiz-cancel">' + ico('arrow-left') + esc(t('Cancel')) + '</button>' +
      '<button class="btn primary" data-act="wiz-create">' + ico('check') + esc(t(editing ? 'Save changes' : 'Create task')) + '</button></div>';

    return '<div class="crumb mb16"><a href="#/jobs" class="muted">' + esc(t('Sync tasks')) + '</a><span class="sep">/</span><b>' + esc(editing ? w.name : t('New task')) + '</b></div>' +
      '<div class="page-head"><div class="titles"><h1 class="page-title">' + esc(t(editing ? 'Edit sync task' : 'New sync task')) + '</h1>' +
      '<p class="page-sub">' + esc(t(editing
        ? 'Change the provider, the records or the schedule. Every field opens with the value this task uses today.'
        : 'Pick an address family, a provider and a set of domains — OzDDNS keeps every record pointed at this host.')) + '</p></div></div>' +
      '<div class="card pad" style="max-width:760px">' + body + foot + '</div>';
  };

  // The IP source field: pick a stored source, or build a new one inline.
  function sourceField(w, savedSources, picked) {
    var head = '<div class="field"><label>' + esc(t('IP source')) + ' <span class="req">*</span></label>';
    var picker = '';
    if (savedSources.length) {
      picker = '<select class="input" data-field="wiz_src_pick" data-act="wiz-src-pick">' +
        '<option value="__new__">' + esc(t('Create a new source')) + '</option>' +
        savedSources.map(function (s) {
          var label = s.name + ' · ' + (s.family === 'v6' ? 'IPv6' : 'IPv4') + ' · ' + s.target;
          return '<option value="' + esc(s.id) + '"' + (picked && picked.id === s.id ? ' selected' : '') + '>' +
            esc(label) + '</option>';
        }).join('') + '</select>';
    }
    if (picked) {
      var st = OZ.data.ipSourceTypes.filter(function (x) { return x.id === picked.type; })[0] || { icon: 'globe' };
      var users = (OZ.data.jobs || []).filter(function (j) { return j.ipSourceId === picked.id; }).length;
      return head + picker +
        '<div class="callout"><div class="hstack wrap" style="gap:10px">' +
        ico(st.icon) + '<span class="strong">' + esc(picked.name) + '</span>' +
        C.pill(picked.family === 'v6' ? 'violet' : 'info', picked.family.toUpperCase()) +
        '<span class="mono tiny muted">' + esc(picked.target) + '</span>' +
        '<button type="button" class="btn sm" data-act="test-source" data-id="' + esc(picked.id) + '">' + ico('refresh') + esc(t('Test')) + '</button>' +
        (users ? C.pill('mute', t('{n} task(s) already use it.', { n: users })) : '') +
        '</div></div>' +
        '<span class="help">' + esc(t('This task shares the saved source — nothing new is stored, and OzDDNS resolves it on every run.')) + '</span></div>';
    }
    return head + picker +
      '<div class="row2">' +
      '<select class="input" data-field="wiz_src_type" data-act="wiz-src-type">' +
      (OZ.data.ipSourceTypes || []).map(function (s) {
        return '<option value="' + esc(s.id) + '"' + (w.sourceType === s.id ? ' selected' : '') + '>' + esc(s.label) + '</option>';
      }).join('') + '</select>' +
      sourceValue(w) + '</div>' +
      '<span class="help">' + esc(sourceHint(w)) + '</span></div>';
  }
})();
