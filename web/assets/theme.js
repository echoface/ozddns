/* ============================================================
   OzDDNS — theme (light / dark / auto)

   Mirrors the i18n module:
   1. `set(pref, persist)` records the preference and paints the
      resolved theme onto <html data-theme="…"> so the design tokens
      in tokens.css flip between the ivory and charcoal palettes.
   2. Resolution order on a cold load: `?theme=` > localStorage >
      the server setting (layered in by the API bridge at bootstrap)
      > "light".
   3. "auto" follows the operating system via prefers-color-scheme and
      re-paints live when the OS switches.
   ============================================================ */
(function () {
  var OZ = window.OZ = window.OZ || {};

  var CODES = ['light', 'dark', 'auto'];
  var LABELS = { light: 'Light', dark: 'Dark', auto: 'Auto' };
  var ALIASES = {
    light: 'light', day: 'light', bright: 'light',
    dark: 'dark', night: 'dark', darkmode: 'dark', 'dark-mode': 'dark',
    auto: 'auto', system: 'auto', os: 'auto', default: 'auto'
  };

  function normalize(v) {
    if (v == null) return null;
    return ALIASES[String(v).trim().toLowerCase()] || null;
  }

  var mq = null;
  try { mq = window.matchMedia('(prefers-color-scheme: dark)'); } catch (e) {}

  var theme = OZ.theme = {
    codes: CODES,
    current: 'light',
    override: false,

    normalize: normalize,
    label: function (code) { return LABELS[code] || LABELS.light; },

    // The theme actually painted right now ("light" | "dark").
    resolved: function () {
      if (theme.current === 'auto') return (mq && mq.matches) ? 'dark' : 'light';
      return theme.current === 'dark' ? 'dark' : 'light';
    },
    isDark: function () { return theme.resolved() === 'dark'; },

    apply: function () {
      try {
        var r = theme.resolved(), el = document.documentElement;
        el.setAttribute('data-theme', r);
        el.setAttribute('data-theme-pref', theme.current);
        el.style.colorScheme = r; // native form controls & scrollbars
      } catch (e) {}
    },

    // Choose a preference (light|dark|auto). `persist` caches it locally so
    // the offline prototype remembers the choice too.
    set: function (pref, persist) {
      var c = normalize(pref) || 'light';
      if (c === theme.current) { theme.apply(); return c; }
      theme.current = c;
      theme.apply();
      if (persist !== false) { try { localStorage.setItem('oz.theme', c); } catch (e) {} }
      if (OZ.notify) OZ.notify();
      return c;
    },

    // Flip between light and dark. From "auto" it snaps to the opposite of
    // whatever the OS is showing, so the click always feels responsive.
    toggle: function () { return theme.set(theme.isDark() ? 'light' : 'dark', true); }
  };

  // Boot as early as possible to avoid a flash of the wrong palette.
  var qs = null;
  try { qs = new URLSearchParams(window.location.search).get('theme'); } catch (e) {}
  var boot = null;
  try { boot = localStorage.getItem('oz.theme'); } catch (e) {}
  theme.current = normalize(qs) || normalize(boot) || 'light';
  theme.override = !!normalize(qs); // a ?theme= pins the choice for the session
  if (normalize(qs)) { try { localStorage.setItem('oz.theme', theme.current); } catch (e) {} }
  theme.apply();

  // Follow the OS live while on "auto".
  if (mq) {
    var onchange = function () { if (theme.current === 'auto') theme.apply(); };
    if (mq.addEventListener) mq.addEventListener('change', onchange);
    else if (mq.addListener) mq.addListener(onchange);
  }

  // Live picker inside the settings panel.
  document.addEventListener('change', function (e) {
    var el = e.target.closest && e.target.closest('[data-theme-pick]');
    if (!el) return;
    var code = theme.set(el.value, true);
    if (OZ.data && OZ.data.settings && OZ.data.settings.general) {
      OZ.data.settings.general.theme = code;
    }
  });
})();
