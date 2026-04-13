/*
 * @cell iframe-widget
 * @version 0.2
 * @about
 *   iframe widget for the Gimbal shell.
 *   Back/forward/refresh only activate when same-origin allows it.
 * @implements gimbal-shell#widget
 */

const STYLE_ID = 'gimbal-iframe-styles';

function ensureStyles() {
  const id = STYLE_ID;
  if (document.getElementById(id)) return;
  const s = document.createElement('style');
  s.id = id;
  s.textContent = `
    .gi-wrap {
      width: 100%; height: 100%;
      display: flex; flex-direction: column;
      overflow: hidden;
      background: #fff;
    }
    .gi-frame {
      flex: 1;
      border: none;
      width: 100%;
      height: 100%;
      display: block;
    }
  `;
  document.head.appendChild(s);
}

export default function createWidget({ name, url = 'about:blank' }) {
  ensureStyles();

  const el = document.createElement('div');
  el.className = 'gi-wrap';

  const frame = document.createElement('iframe');
  frame.className = 'gi-frame';
  frame.setAttribute('sandbox', 'allow-scripts allow-same-origin allow-forms allow-popups allow-popups-to-escape-sandbox');
  frame.src = url;
  el.appendChild(frame);

  let controls = null;

  // ── check same-origin access ──────────────────────────
  function canAccessHistory() {
    try {
      void frame.contentWindow.history.length;
      return true;
    } catch {
      return false;
    }
  }

  function setNavEnabled(enabled) {
    for (const btn of [backBtn, forwardBtn, refreshBtn]) {
      if (btn._el) {
        btn._el.disabled      = !enabled;
        btn._el.style.opacity = enabled ? '1' : '0.3';
      }
    }
  }

  // ── button descriptors ────────────────────────────────
  const backBtn = {
    icon: 'back', label: 'back',
    action() { frame.contentWindow.history.back(); },
  };
  const forwardBtn = {
    icon: 'forward', label: 'forward',
    action() { frame.contentWindow.history.forward(); },
  };
  const refreshBtn = {
    icon: { svg: `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round" stroke-width="1.7">
      <polyline points="23 4 23 10 17 10"/>
      <path d="M20.49 15a9 9 0 1 1-2.12-9.36L23 10"/>
    </svg>` },
    label: 'refresh',
    action() { frame.contentWindow.location.reload(); },
  };

  const decoration = {
    icon: { svg: `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round" stroke-width="1.7">
      <rect x="3" y="3" width="18" height="18" rx="2"/>
      <line x1="3" y1="8" x2="21" y2="8"/>
      <line x1="7" y1="5.5" x2="7" y2="5.51"/>
      <line x1="10" y1="5.5" x2="10" y2="5.51"/>
    </svg>` },
    title: name,
    leftButtons: [backBtn, forwardBtn, refreshBtn],
  };

  function shortenUrl(href) {
    try {
      const u = new URL(href);
      return u.hostname + (u.pathname !== '/' ? u.pathname : '');
    } catch {
      return href;
    }
  }

  frame.addEventListener('load', () => {
    const allowed = canAccessHistory();

    // Cross-origin or blank — disable all nav
    if (!allowed) {
      setNavEnabled(false);
      return;
    }

    // Same-origin — update title
    try {
      const title = frame.contentDocument?.title;
      if (title) controls?.setTitle(title);
      else controls?.setTitle(shortenUrl(frame.contentWindow.location.href));
    } catch {
      controls?.setTitle(name);
    }

    // Same-origin — smart back/forward based on history position
    // We track cursor ourselves since history.index isn't exposed
    const len = frame.contentWindow.history.length;

    // On a new load, cursor advances
    cursor = Math.min(cursor + 1, len - 1);

    if (backBtn._el) {
      backBtn._el.disabled      = cursor <= 0;
      backBtn._el.style.opacity = cursor > 0 ? '1' : '0.3';
    }
    if (forwardBtn._el) {
      forwardBtn._el.disabled      = cursor >= len - 1;
      forwardBtn._el.style.opacity = cursor < len - 1 ? '1' : '0.3';
    }
    if (refreshBtn._el) {
      refreshBtn._el.disabled      = false;
      refreshBtn._el.style.opacity = '1';
    }
  });

  return {
    el,
    decoration,

    get controls() { return controls; },
    set controls(c) { controls = c; },

    focus()   { frame.focus(); },
    destroy() { frame.src = 'about:blank'; },
  };
}