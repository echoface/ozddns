/* OzDDNS homepage — progressive enhancement only. */
(function () {
  "use strict";

  var root = document.documentElement;
  var STORAGE_LANG = "oz-lang";
  var STORAGE_THEME = "oz-theme";

  var strings = {
    en: {
      copy: "Copy",
      copied: "Copied",
      copyAria: "Copy code",
      lang: "Switch language",
      themeToLight: "Switch to light theme",
      themeToDark: "Switch to dark theme",
      menuOpen: "Open menu",
      menuClose: "Close menu",
      title: "OzDDNS — DNS records that stay in sync",
      description:
        "OzDDNS is a provider-centric dynamic DNS manager. It discovers your address, reads the live record, and writes only when the two disagree. One Go binary with a web panel, REST API, CLI, scheduler, and SQLite.",
    },
    zh: {
      copy: "复制",
      copied: "已复制",
      copyAria: "复制代码",
      lang: "切换语言",
      themeToLight: "切换到亮色主题",
      themeToDark: "切换到暗色主题",
      menuOpen: "打开菜单",
      menuClose: "关闭菜单",
      title: "OzDDNS — 始终同步的 DNS 记录",
      description:
        "OzDDNS 是以服务商为中心的动态 DNS 管理器：发现地址、读取线上记录，只在两者不一致时写入。单个 Go 二进制内置管理面板、REST API、CLI、调度器与 SQLite 存储。",
    },
  };

  function readStorage(key) {
    try {
      return localStorage.getItem(key);
    } catch (e) {
      return null;
    }
  }

  function writeStorage(key, value) {
    try {
      localStorage.setItem(key, value);
    } catch (e) {
      /* storage may be unavailable */
    }
  }

  var currentLang = "en";

  function applyLanguage(lang) {
    currentLang = lang === "zh" ? "zh" : "en";
    var dict = strings[currentLang];

    root.lang = currentLang === "zh" ? "zh-Hans" : "en";

    document.querySelectorAll("[data-en]").forEach(function (el) {
      var value = el.getAttribute("data-" + currentLang);
      if (value !== null) {
        el.innerHTML = value;
      }
    });

    var langBtn = document.getElementById("lang-toggle");
    if (langBtn) {
      langBtn.textContent = currentLang === "zh" ? "EN" : "中文";
      langBtn.setAttribute("aria-label", dict.lang);
      langBtn.setAttribute("title", dict.lang);
    }

    var meta = document.getElementById("meta-desc");
    if (meta) meta.setAttribute("content", dict.description);

    document.title = dict.title;

    document.querySelectorAll("[data-copy]").forEach(function (btn) {
      btn.setAttribute("aria-label", dict.copyAria);
      if (btn.classList.contains("is-done")) return;
      var label = btn.querySelector(".copy-label");
      if (label) label.textContent = dict.copy;
    });

    syncThemeButton();
    syncMenuButton();

    writeStorage(STORAGE_LANG, currentLang);
  }

  /* ---------------------------------------------------------
     Theme
     --------------------------------------------------------- */

  var themeMeta = document.querySelector('meta[name="theme-color"]');

  function syncThemeButton() {
    var btn = document.getElementById("theme-toggle");
    if (!btn) return;
    var dict = strings[currentLang];
    var isDark = root.dataset.theme !== "light";
    var label = isDark ? dict.themeToLight : dict.themeToDark;
    btn.setAttribute("aria-label", label);
    btn.setAttribute("title", label);
    if (themeMeta) {
      themeMeta.setAttribute("content", isDark ? "#06080b" : "#f6f8fa");
    }
  }

  var themeBtn = document.getElementById("theme-toggle");
  if (themeBtn) {
    themeBtn.addEventListener("click", function () {
      var next = root.dataset.theme === "light" ? "dark" : "light";
      root.dataset.theme = next;
      writeStorage(STORAGE_THEME, next);
      syncThemeButton();
    });
  }

  /* ---------------------------------------------------------
     Language
     --------------------------------------------------------- */

  var langBtn = document.getElementById("lang-toggle");
  if (langBtn) {
    langBtn.addEventListener("click", function () {
      applyLanguage(currentLang === "zh" ? "en" : "zh");
    });
  }

  var storedLang = readStorage(STORAGE_LANG);
  var initialLang =
    storedLang === "zh" || storedLang === "en"
      ? storedLang
      : (navigator.language || "").toLowerCase().indexOf("zh") === 0
        ? "zh"
        : "en";

  applyLanguage(initialLang);

  /* ---------------------------------------------------------
     Copy buttons
     --------------------------------------------------------- */

  document.querySelectorAll("[data-copy]").forEach(function (btn) {
    btn.addEventListener("click", function () {
      var scope = btn.closest(".term, .cli-strip, .tab-panel");
      var code = scope ? scope.querySelector("pre code") : null;
      var text = code ? code.textContent : "";

      function done() {
        var label = btn.querySelector(".copy-label");
        btn.classList.add("is-done");
        if (label) label.textContent = strings[currentLang].copied;
        window.setTimeout(function () {
          btn.classList.remove("is-done");
          if (label) label.textContent = strings[currentLang].copy;
        }, 1600);
      }

      if (navigator.clipboard && window.isSecureContext) {
        navigator.clipboard.writeText(text).then(done, fallback);
      } else {
        fallback();
      }

      function fallback() {
        var area = document.createElement("textarea");
        area.value = text;
        area.setAttribute("readonly", "");
        area.style.position = "fixed";
        area.style.opacity = "0";
        document.body.appendChild(area);
        area.select();
        try {
          document.execCommand("copy");
          done();
        } catch (e) {
          /* leave the label untouched */
        }
        document.body.removeChild(area);
      }
    });
  });

  /* ---------------------------------------------------------
     Tabs
     --------------------------------------------------------- */

  var tablists = document.querySelectorAll('[role="tablist"]');

  tablists.forEach(function (list) {
    var tabs = Array.prototype.slice.call(list.querySelectorAll('[role="tab"]'));

    function select(tab, focus) {
      tabs.forEach(function (item) {
        var active = item === tab;
        item.classList.toggle("is-active", active);
        item.setAttribute("aria-selected", active ? "true" : "false");
        item.tabIndex = active ? 0 : -1;
        var panel = document.getElementById(item.getAttribute("aria-controls"));
        if (panel) {
          panel.hidden = !active;
          panel.classList.toggle("is-active", active);
        }
      });
      if (focus) tab.focus();
    }

    tabs.forEach(function (tab, index) {
      tab.addEventListener("click", function () {
        select(tab, false);
      });

      tab.addEventListener("keydown", function (event) {
        var next = null;
        if (event.key === "ArrowRight" || event.key === "ArrowDown") {
          next = tabs[(index + 1) % tabs.length];
        } else if (event.key === "ArrowLeft" || event.key === "ArrowUp") {
          next = tabs[(index - 1 + tabs.length) % tabs.length];
        } else if (event.key === "Home") {
          next = tabs[0];
        } else if (event.key === "End") {
          next = tabs[tabs.length - 1];
        }
        if (next) {
          event.preventDefault();
          select(next, true);
        }
      });
    });
  });

  /* ---------------------------------------------------------
     Mobile menu
     --------------------------------------------------------- */

  var menuBtn = document.getElementById("menu-toggle");
  var nav = document.getElementById("nav");

  function syncMenuButton() {
    if (!menuBtn || !nav) return;
    var open = nav.classList.contains("open");
    var dict = strings[currentLang];
    menuBtn.setAttribute("aria-expanded", open ? "true" : "false");
    var label = open ? dict.menuClose : dict.menuOpen;
    menuBtn.setAttribute("aria-label", label);
    menuBtn.setAttribute("title", label);
  }

  function closeMenu() {
    if (!nav) return;
    nav.classList.remove("open");
    syncMenuButton();
  }

  if (menuBtn && nav) {
    menuBtn.addEventListener("click", function () {
      nav.classList.toggle("open");
      syncMenuButton();
    });

    nav.addEventListener("click", function (event) {
      if (event.target.closest("a")) closeMenu();
    });

    document.addEventListener("keydown", function (event) {
      if (event.key === "Escape" && nav.classList.contains("open")) {
        closeMenu();
        menuBtn.focus();
      }
    });

    window.addEventListener("resize", function () {
      if (window.innerWidth > 860) closeMenu();
    });

    syncMenuButton();
  }

  /* ---------------------------------------------------------
     Header state
     --------------------------------------------------------- */

  var header = document.querySelector(".site-header");

  function onScroll() {
    if (!header) return;
    header.classList.toggle("is-scrolled", window.scrollY > 8);
  }

  window.addEventListener("scroll", onScroll, { passive: true });
  onScroll();

  /* ---------------------------------------------------------
     Reveal on scroll
     --------------------------------------------------------- */

  var revealItems = document.querySelectorAll(".reveal");

  if ("IntersectionObserver" in window) {
    var observer = new IntersectionObserver(
      function (entries) {
        entries.forEach(function (entry) {
          if (entry.isIntersecting) {
            entry.target.classList.add("is-visible");
            observer.unobserve(entry.target);
          }
        });
      },
      { rootMargin: "0px 0px -8% 0px", threshold: 0.08 },
    );

    revealItems.forEach(function (item) {
      observer.observe(item);
    });
  } else {
    revealItems.forEach(function (item) {
      item.classList.add("is-visible");
    });
  }

  /* ---------------------------------------------------------
     Footer year
     --------------------------------------------------------- */

  var year = document.getElementById("year");
  if (year) year.textContent = String(new Date().getFullYear());
})();
