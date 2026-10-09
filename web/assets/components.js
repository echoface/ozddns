/* ============================================================
   OzDDNS — presentational component builders (return HTML strings)
   ============================================================ */
(function () {
  var OZ = window.OZ, ico = window.ico, U = OZ.util, esc = U.esc;
  var C = OZ.c = {};

  // A value produced by the server's MaskConfig (e.g. "••••2481"). The panel
  // never receives the real credential, so stored secrets are shown as an
  // empty input plus a "Saved" chip instead of pre-filling the mask.
  function looksMasked(v) { return typeof v === 'string' && v.indexOf('••') >= 0; }

  C.statusMeta = {
    success:  { cls: 'ok',   label: 'Updated',   ico: 'check-circle' },
    nochange: { cls: 'mute', label: 'No change', ico: 'circle-dot' },
    failed:   { cls: 'err',  label: 'Failed',    ico: 'x-circle' },
    skipped:  { cls: 'info', label: 'Skipped',   ico: 'corner-down-right' },
    running:  { cls: 'coral',label: 'Running',   ico: 'loader' }
  };

  C.pill = function (cls, text, dot) {
    return '<span class="pill ' + cls + '">' + (dot ? '<span class="dot"></span>' : '') + esc(text) + '</span>';
  };
  C.runPill = function (status) {
    var m = C.statusMeta[status] || C.statusMeta.nochange;
    return '<span class="pill ' + m.cls + '">' + ico(m.ico) + esc(m.label) + '</span>';
  };

  C.providerBadge = function (providerId, big) {
    var p = OZ.get.provider(providerId);
    var t = OZ.get.providerType(p ? p.type : 'custom');
    var initials = (t.label.match(/[A-Za-z]/g) || []).slice(0, 2).join('').toUpperCase();
    return '<span class="pv">' +
      '<span class="logo ' + (big ? 'lg' : '') + '" style="color:var(--pv-' + t.color + ');background:var(--pv-' + t.color + '-bg)">' +
      esc(initials) + '</span>' +
      '<span><span class="pv-name">' + esc(p ? p.name : 'Unknown') + '</span><br>' +
      '<span class="pv-type">' + esc(t.label) + '</span></span></span>';
  };

  C.providerLogo = function (type, big) {
    var t = OZ.get.providerType(type) || { label: type, color: 'custom' };
    var initials = (t.label.match(/[A-Za-z]/g) || []).slice(0, 2).join('').toUpperCase();
    return '<span class="logo ' + (big ? 'lg' : '') + '" style="color:var(--pv-' + t.color + ');background:var(--pv-' + t.color + '-bg)">' + esc(initials) + '</span>';
  };

  C.providerStatus = function (p) {
    if (!p.enabled) return C.pill('mute', 'Disabled', true);
    if (p.status === 'ok') return C.pill('ok', 'Connected', true);
    if (p.status === 'warn') return C.pill('warn', 'Attention', true);
    return C.pill('err', 'Error', true);
  };

  C.rtype = function (type) { return '<span class="rtype ' + esc(type) + '">' + esc(type) + '</span>'; };

  C.diff = function (oldIp, newIp) {
    if (oldIp === newIp) return '<span class="mono" style="color:var(--ink-2)">' + esc(newIp || '—') + '</span>';
    return '<span class="diff"><span class="old">' + esc(oldIp || '—') + '</span><span class="arw">→</span><span class="new">' + esc(newIp || '—') + '</span></span>';
  };

  C.stat = function (lab, val, foot, icon, spark) {
    return '<div class="card stat">' +
      '<span class="lab">' + (icon ? ico(icon) : '') + esc(lab) + '</span>' +
      '<span class="val">' + val + '</span>' +
      (foot ? '<span class="foot">' + foot + '</span>' : '') +
      (spark ? '<span class="spark">' + spark + '</span>' : '') +
      '</div>';
  };

  C.empty = function (icon, title, body, actionHtml) {
    return '<div class="empty"><div class="art">' + ico(icon) + '</div>' +
      '<h3>' + esc(title) + '</h3><p>' + body + '</p>' + (actionHtml || '') + '</div>';
  };

  C.banner = function (kind, title, body, icon) {
    return '<div class="banner ' + kind + '">' + ico(icon || (kind === 'err' ? 'alert-triangle' : kind === 'ok' ? 'check-circle' : kind === 'warn' ? 'alert-triangle' : 'info')) +
      '<div><div class="b-title">' + esc(title) + '</div>' + (body ? '<div class="b-body">' + body + '</div>' : '') + '</div></div>';
  };

  // opts.act turns each step into a button that fires OZ.actions[act] with
  // data-id = step index, so a wizard can jump straight to a step.
  C.stepper = function (steps, current, opts) {
    var act = opts && opts.act;
    return '<div class="stepper">' + steps.map(function (s, i) {
      var cls = i === current ? 'on' : i < current ? 'done' : '';
      var inner = '<span class="n">' + (i < current ? '✓' : (i + 1)) + '</span>' + esc(s);
      var step = act
        ? '<button type="button" class="step ' + cls + '" data-act="' + act + '" data-id="' + i + '"' +
          (i === current ? ' aria-current="step"' : '') + '>' + inner + '</button>'
        : '<span class="step ' + cls + '">' + inner + '</span>';
      return (i ? '<span class="line"></span>' : '') + step;
    }).join('') + '</div>';
  };

  C.field = function (opts) {
    var f = opts.field || {};
    var id = 'f_' + (opts.key || f.key || Math.random().toString(36).slice(2));
    var labelText = (opts.label != null) ? opts.label : f.label;
    var label = '<label for="' + id + '">' + esc(labelText || '') + (f.required ? ' <span class="req">*</span>' : '') + '</label>';
    var help = (f.help || opts.help) ? '<span class="help">' + esc(f.help || opts.help) + '</span>' : '';
    var key = opts.key || f.key;
    var val = (OZ.form[key] != null) ? OZ.form[key]
      : (opts.value != null ? opts.value : (f.default != null ? f.default : ''));
    var ctrl;
    var df = key ? ' data-field="' + esc(key) + '"' : '';
    if (f.type === 'secret') {
      // Never send a stored credential (or its mask) to the browser: render the
      // input empty and surface the masked tail in a chip so the user can see
      // that a value is on file. Typing a new value replaces it.
      var stored = looksMasked(opts.value) ? String(opts.value) : (looksMasked(f.default) ? String(f.default) : '');
      var sval = looksMasked(val) ? '' : val;
      ctrl = '<div class="input-group"><input id="' + id + '"' + df + ' class="input mono" type="password" autocomplete="new-password" placeholder="' + esc(opts.placeholder || '••••••••') + '" value="' + esc(sval) + '">' +
        '<button class="btn peek' + (sval ? '' : ' is-empty') + '" type="button" data-act="peek" title="' + esc(OZ.t('Show / hide')) + '" aria-label="' + esc(OZ.t('Show / hide')) + '">' + ico('eye') + '</button></div>';
      if (stored) {
        ctrl += '<div class="secret-note"><span class="pill mute"><span class="dot"></span>' +
          '<span>Saved</span><span class="mono">' + esc(stored) + '</span></span>' +
          '<span class="tiny muted">Leave blank to keep it, or type a new value to replace.</span></div>';
      }
    } else if (f.type === 'textarea') {
      ctrl = '<textarea id="' + id + '"' + df + ' class="input mono" rows="3">' + esc(val) + '</textarea>';
    } else if (f.type === 'select') {
      ctrl = '<select id="' + id + '"' + df + ' class="input">' + (f.options || []).map(function (o) {
        return '<option' + (o === val ? ' selected' : '') + '>' + esc(o) + '</option>';
      }).join('') + '</select>';
    } else if (f.type === 'bool') {
      // Booleans render as an explicit true/false select so the value that is
      // captured (and sent to the API) is always unambiguous.
      var truthy = (val === true || val === 'true' || val === 1 || val === '1' || val === 'on');
      ctrl = '<select id="' + id + '"' + df + ' class="input">' +
        '<option value="true"' + (truthy ? ' selected' : '') + '>Yes</option>' +
        '<option value="false"' + (truthy ? '' : ' selected') + '>No</option>' +
        '</select>';
    } else if (f.type === 'number') {
      ctrl = '<input id="' + id + '"' + df + ' class="input mono" type="number" value="' + esc(val) + '">';
    } else {
      ctrl = '<input id="' + id + '"' + df + ' class="input' + (f.mono ? ' mono' : '') + '" type="text"' +
        (opts.list ? ' list="' + esc(opts.list) + '"' : '') +
        ' placeholder="' + esc(opts.placeholder || '') + '" value="' + esc(val) + '">';
    }
    return '<div class="field"><label>' + label + '</label>' + ctrl + help + '</div>';
  };
  C.switchRow = function (title, desc, on, act, id) {
    return '<div class="switch-row"><div class="txt"><div class="t">' + esc(title) + '</div><div class="d">' + esc(desc) + '</div></div>' +
      '<button class="switch ' + (on ? 'on' : '') + '" data-act="' + act + '"' + (id ? ' data-id="' + id + '"' : '') + ' aria-pressed="' + (!!on) + '"></button></div>';
  };

  C.toggle = function (on, act, id, label) {
    return '<button class="switch ' + (on ? 'on' : '') + '" data-act="' + act + '"' + (id ? ' data-id="' + id + '"' : '') + ' title="' + esc(label || (on ? 'Enabled' : 'Disabled')) + '"></button>';
  };

  // A toggle that participates in form capture: its value is read from the
  // element's `on` class by the API bridge (data-type="bool"), so it can be
  // saved alongside the rest of a settings card.
  C.switchField = function (key, on, title, desc) {
    return '<div class="switch-row"><div class="txt"><div class="t">' + esc(title) + '</div>' +
      (desc ? '<div class="d">' + esc(desc) + '</div>' : '') + '</div>' +
      '<button type="button" class="switch ' + (on ? 'on' : '') + '" data-act="switch" data-field="' + esc(key) + '" data-type="bool" aria-pressed="' + (!!on) + '"></button></div>';
  };

  /* tiny inline sparkline SVG */
  C.spark = function (color, points) {
    var w = 84, h = 26, max = Math.max.apply(null, points), min = Math.min.apply(null, points);
    var d = points.map(function (p, i) {
      var x = (i / (points.length - 1)) * w, y = h - ((p - min) / (max - min || 1)) * (h - 5) - 2.5;
      return (i ? 'L' : 'M') + x.toFixed(1) + ' ' + y.toFixed(1);
    }).join(' ');
    return '<svg width="' + w + '" height="' + h + '" viewBox="0 0 ' + w + ' ' + h + '"><path d="' + d + '" fill="none" stroke="var(--' + color + ')" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"/></svg>';
  };
})();
