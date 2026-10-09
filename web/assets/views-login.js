/* OzDDNS — sign-in screen */
(function () {
  var OZ = window.OZ, ico = window.ico, U = OZ.util, esc = U.esc;
  var t = function (k, v) { return OZ.t(k, v); };
  OZ.actions['login'] = function () {
    var p = OZ.form['login_pass'];
    if (p && p.length < 4) { OZ.set({ authPasswordError: true }); return; }
    OZ.set({ authPasswordError: false });
    OZ.toast('ok', t('Welcome back'), t('Signed in as {user}', { user: (OZ.form['login_user'] || 'admin') }));
    OZ.navigate('#/overview');
  };

  OZ.views.login = function () {
    return '<div class="auth"><div class="aside">' +
      '<div class="hstack" style="gap:11px;position:relative;z-index:1">' +
      '<span class="mark" style="width:36px;height:36px;border-radius:11px;background:radial-gradient(circle at 30% 25%,#E89A7C,#D97757 62%);display:grid;place-items:center;font-family:var(--font-display);font-weight:700;color:#fff">O</span>' +
      '<span style="font-family:var(--font-display);font-size:20px;font-weight:600">Oz<span style="color:var(--coral)">DDNS</span></span></div>' +
      '<div><div class="quote">Your hosts move. Your DNS follows, quietly.</div>' +
      '<p class="qsub">A provider-centric dynamic DNS engine — reconcile the address your machines actually hold with the records the world resolves, and never guess again.</p></div>' +
      '<div class="hstack" style="gap:18px;color:rgba(243,239,230,.55);font-size:12px;position:relative;z-index:1">' +
      '<span>7 providers</span><span>·</span><span>single binary</span><span>·</span><span>local-first</span></div>' +
      '</div>' +
      '<div class="form-side"><div class="fbox">' +
      '<h1 class="page-title" style="font-size:26px">Sign in</h1>' +
      '<p class="page-sub mb24">Authenticate to manage providers, tasks and records.</p>' +
      C_field('user', 'Username', 'text', 'admin') +
      C_field('pass', 'Password', 'password', '') +
      (OZ.state.authPasswordError ? '<div class="err-msg mb12" style="color:var(--rust);font-size:12.5px;display:flex;gap:6px;align-items:center">' + ico('alert-triangle') + 'Password must be at least 4 characters.</div>' : '') +
      '<div class="between mb16"><label class="hstack tiny muted" style="gap:7px"><input type="checkbox" checked style="accent-color:var(--coral)">Keep me signed in</label>' +
      '<a class="tiny" style="color:var(--coral-700)">Reset password</a></div>' +
      '<button class="btn primary block lg" data-act="login">' + ico('log-in') + 'Sign in</button>' +
      '<div class="divider"></div>' +
      '<p class="tiny muted" style="text-align:center">No credentials yet? A fresh install opens without auth on <span class="mono">127.0.0.1</span>.</p>' +
      '</div></div></div>';
  };

  function C_field(key, label, type, val) {
    return '<div class="field"><label>' + esc(label) + '</label>' +
      '<input class="input mono" type="' + type + '" data-field="login_' + key + '" value="' + esc(val) + '" placeholder="' + (type === 'password' ? '••••••••••' : '') + '"></div>';
  }
})();
