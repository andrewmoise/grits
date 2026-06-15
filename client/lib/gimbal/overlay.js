/**
 * lib/gimbal/overlay.js — Overlay window manager
 *
 * A drop-in replacement for the full gwm tiling session. Installs itself at
 * window.gimbal with the same openWidget() interface that tool modules call,
 * so any tool (gterm, codemirror, files, …) works unchanged.
 *
 * One widget is visible at a time. Opening a new widget minimizes the current
 * one (preserving its state). Minimized widgets are kept in memory but hidden.
 *
 * Usage from the browser console on any page served from the same origin
 * (or a CORS-enabled origin):
 *
 *   await import('https://yourserver.com/grits/v1/content/client/lib/gimbal/overlay.js');
 *   // window.gwm is now available:
 *   await gwm.eval('gterm()');
 *   await gwm.eval('edit("lib/foo/main.js")');
 *   gwm.hide();
 *   gwm.show();
 *   gwm.toggle();
 */

import GritsClient     from '../grits/GritsClient.js';
import { makeShell }   from '../gimbal/gsh.js';
import { injectTheme } from '../style/style.js';

// Inject design-system CSS vars onto whatever host page we land on.
injectTheme();

// ── Layout constants ──────────────────────────────────────────────────────────
const MIN_W       = 320;
const MIN_H       = 200;
const DEFAULT_W   = 600;
const DEFAULT_H   = 400;
const EDGE_OFFSET = 16;  // px from right/bottom of viewport

// ── SVG icons (matching gwm.html) ────────────────────────────────────────────
const ICONS = {
  gterm:    `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round"><polyline points="4 17 10 11 4 5"/><line x1="12" y1="19" x2="20" y2="19"/></svg>`,
  files:    `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round"><path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z"/></svg>`,
  editor:   `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round"><path d="M11 4H4a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2v-7"/><path d="M18.5 2.5a2.121 2.121 0 0 1 3 3L12 15l-4 1 1-4 9.5-9.5z"/></svg>`,
  minimize: `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round"><line x1="5" y1="19" x2="19" y2="19"/></svg>`,
  close:    `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg>`,
};

function _iconSvg(name) {
  if (name && typeof name === 'object' && name.svg) return name.svg;
  return ICONS[name] ?? ICONS.gterm;
}

// ── Styles ────────────────────────────────────────────────────────────────────
const STYLE_ID = 'gimbal-overlay-styles';
if (!document.getElementById(STYLE_ID)) {
  const s = document.createElement('style');
  s.id = STYLE_ID;
  s.textContent = `
    #gimbal-overlay {
      position: fixed;
      bottom: ${EDGE_OFFSET}px;
      right:  ${EDGE_OFFSET}px;
      z-index: 2147483647;
      display: flex;
      flex-direction: column;
      background: var(--bg-float);
      border: 1px solid var(--border);
      border-radius: var(--widget-r, 0.5rem);
      overflow: hidden;
      box-shadow:
        0 0 0 1px rgba(0,0,0,0.5),
        0 0.4rem 1.5rem rgba(0,0,0,0.45),
        0 0.1rem 0.3rem rgba(0,0,0,0.3);
    }
    #gimbal-overlay:focus-within {
      border-color: var(--border-focus);
      box-shadow:
        0 0 0 1px rgba(0,0,0,0.5),
        0 0.6rem 1.75rem rgba(0,0,0,0.55),
        0 0.1rem 0.3rem rgba(0,0,0,0.3);
    }
    #gimbal-overlay-title {
      height: var(--title-h, 1.875rem);
      background: var(--bg-elevated);
      border-bottom: 1px solid var(--border);
      display: flex;
      align-items: center;
      padding: 0 0.3rem;
      flex-shrink: 0;
      cursor: grab;
      user-select: none;
      border-radius: var(--widget-r, 0.5rem) var(--widget-r, 0.5rem) 0 0;
      transition: background 0.15s;
    }
    #gimbal-overlay-title:active { cursor: grabbing; }
    #gimbal-overlay:focus-within #gimbal-overlay-title { background: var(--bg-active); }
    #gimbal-overlay-title.is-dirty #gimbal-overlay-name { color: var(--a2, #4ade80); }
    #gimbal-overlay-icon {
      width: var(--btn-size, 1.875rem);
      height: var(--btn-size, 1.875rem);
      display: flex; align-items: center; justify-content: center;
      color: var(--text-dim);
      flex-shrink: 0;
      transition: color 0.15s;
    }
    #gimbal-overlay-icon svg { width: var(--btn-svg-sz, 0.98em); height: var(--btn-svg-sz, 0.98em); stroke-width: 1.7; }
    #gimbal-overlay:focus-within #gimbal-overlay-icon { color: var(--text); }
    #gimbal-overlay-name {
      flex: 1;
      text-align: center;
      font-size: var(--fs-md, 0.7rem);
      font-weight: 400;
      color: var(--text-dim);
      letter-spacing: 0.02em;
      white-space: nowrap;
      overflow: hidden;
      text-overflow: ellipsis;
      padding: 0 0.25rem;
      transition: color 0.15s;
      font-family: var(--font-ui);
    }
    #gimbal-overlay:focus-within #gimbal-overlay-name { color: var(--text); }
    .gimbal-overlay-btn {
      width: var(--btn-size, 1.875rem);
      height: var(--btn-size, 1.875rem);
      border-radius: 0.35rem;
      border: none;
      background: transparent;
      color: var(--text-dim);
      cursor: pointer;
      display: flex; align-items: center; justify-content: center;
      padding: 0;
      flex-shrink: 0;
      transition: background 0.12s, color 0.12s;
    }
    .gimbal-overlay-btn:hover { background: var(--bg-hover); color: var(--text-hi); }
    .gimbal-overlay-btn.close:hover { background: var(--red-dim); color: var(--red); }
    .gimbal-overlay-btn svg { width: var(--btn-svg-sz, 0.98em); height: var(--btn-svg-sz, 0.98em); stroke-width: 1.7; }
    #gimbal-overlay-body {
      flex: 1;
      min-height: 0;
      display: flex;
      flex-direction: column;
      overflow: hidden;
    }
    .gimbal-overlay-rh {
      position: absolute;
      z-index: 10;
    }
    .gimbal-overlay-rh.edge-left {
      left: 0; top: 8px; bottom: 0;
      width: 5px;
      cursor: w-resize;
    }
    .gimbal-overlay-rh.edge-top {
      top: 0; left: 8px; right: 0;
      height: 5px;
      cursor: n-resize;
    }
    .gimbal-overlay-rh.edge-topleft {
      top: 0; left: 0;
      width: 10px; height: 10px;
      cursor: nw-resize;
    }
  `;
  document.head.appendChild(s);
}

// ── Overlay DOM ───────────────────────────────────────────────────────────────
// Built once on first use; mounted/unmounted from document.body as needed.

let _overlayEl = null;
let _titleEl   = null;
let _iconEl    = null;
let _nameEl    = null;
let _bodyEl    = null;
let _overlayW  = DEFAULT_W;
let _overlayH  = DEFAULT_H;

function _ensureDOM() {
  if (_overlayEl) return;

  const wrap = document.createElement('div');
  wrap.id = 'gimbal-overlay';
  wrap.style.width  = _overlayW + 'px';
  wrap.style.height = _overlayH + 'px';

  // title bar
  const title = document.createElement('div');
  title.id = 'gimbal-overlay-title';

  const icon = document.createElement('div');
  icon.id = 'gimbal-overlay-icon';
  icon.innerHTML = ICONS.gterm;

  const name = document.createElement('span');
  name.id = 'gimbal-overlay-name';

  const minimizeBtn = document.createElement('button');
  minimizeBtn.className = 'gimbal-overlay-btn';
  minimizeBtn.innerHTML = ICONS.minimize;
  minimizeBtn.title = 'minimize';
  minimizeBtn.addEventListener('click', e => { e.stopPropagation(); hide(); });

  const closeBtn = document.createElement('button');
  closeBtn.className = 'gimbal-overlay-btn close';
  closeBtn.innerHTML = ICONS.close;
  closeBtn.title = 'close';
  closeBtn.addEventListener('click', e => { e.stopPropagation(); _destroyCurrent(); });

  title.appendChild(icon);
  title.appendChild(name);
  title.appendChild(minimizeBtn);
  title.appendChild(closeBtn);
  wrap.appendChild(title);

  // body
  const body = document.createElement('div');
  body.id = 'gimbal-overlay-body';
  wrap.appendChild(body);

  // resize handles
  for (const cls of ['edge-topleft', 'edge-top', 'edge-left']) {
    const h = document.createElement('div');
    h.className = `gimbal-overlay-rh ${cls}`;
    wrap.appendChild(h);
  }

  _overlayEl = wrap;
  _titleEl   = title;
  _iconEl    = icon;
  _nameEl    = name;
  _bodyEl    = body;

  _bindTitleDrag();
  _bindResizeHandles();
}

function _bindTitleDrag() {
  _titleEl.addEventListener('mousedown', e => {
    if (e.target.closest('.gimbal-overlay-btn')) return;
    e.preventDefault();
    let lastX = e.clientX, lastY = e.clientY;

    function onMove(e) {
      const dx = e.clientX - lastX;
      const dy = e.clientY - lastY;
      lastX = e.clientX;
      lastY = e.clientY;
      const r = parseFloat(_overlayEl.style.right)  || EDGE_OFFSET;
      const b = parseFloat(_overlayEl.style.bottom) || EDGE_OFFSET;
      _overlayEl.style.right  = Math.max(0, r - dx) + 'px';
      _overlayEl.style.bottom = Math.max(0, b - dy) + 'px';
    }

    document.body.style.cursor = 'grabbing';
    document.addEventListener('mousemove', onMove);
    document.addEventListener('mouseup', () => {
      document.removeEventListener('mousemove', onMove);
      document.body.style.cursor = '';
    }, { once: true });
  });
}

function _bindResizeHandles() {
  _overlayEl.querySelectorAll('.gimbal-overlay-rh').forEach(handle => {
    handle.addEventListener('mousedown', e => {
      e.preventDefault();
      e.stopPropagation();
      const isLeft = handle.classList.contains('edge-left') || handle.classList.contains('edge-topleft');
      const isTop  = handle.classList.contains('edge-top')  || handle.classList.contains('edge-topleft');
      const startX = e.clientX, startY = e.clientY;
      const startW = parseFloat(_overlayEl.style.width);
      const startH = parseFloat(_overlayEl.style.height);

      function onMove(e) {
        if (isLeft) {
          // Right edge stays fixed; grow leftward
          const newW = Math.max(MIN_W, startW - (e.clientX - startX));
          _overlayEl.style.width = newW + 'px';
          _overlayW = newW;
        }
        if (isTop) {
          // Bottom edge stays fixed; grow upward
          const newH = Math.max(MIN_H, startH - (e.clientY - startY));
          _overlayEl.style.height = newH + 'px';
          _overlayH = newH;
        }
      }

      document.addEventListener('mousemove', onMove);
      document.addEventListener('mouseup', () => {
        document.removeEventListener('mousemove', onMove);
      }, { once: true });
    });
  });
}

// ── Decoration sync ───────────────────────────────────────────────────────────
function _syncDecoration(entry) {
  const dec = entry.instance?.decoration ?? {};
  _nameEl.textContent = dec.title ?? entry.name;
  _iconEl.innerHTML   = _iconSvg(dec.icon ?? entry.icon);
  _titleEl.classList.toggle('is-dirty', !!dec.dirty);
}

// ── Controls shim ─────────────────────────────────────────────────────────────
// Matches the interface gwm.html provides to widget instances.
function _makeControls(entry) {
  return {
    update() {
      // Only sync if this entry is the currently visible one
      const cur = _currentVisible();
      if (cur === entry) _syncDecoration(entry);
    },
    setTitle(str) {
      const dec = entry.instance?.decoration ?? {};
      dec.title = str;
      const cur = _currentVisible();
      if (cur === entry) _nameEl.textContent = str;
    },
    setDirty(isDirty) {
      const dec = entry.instance?.decoration ?? {};
      dec.dirty = isDirty;
      const cur = _currentVisible();
      if (cur === entry) _titleEl.classList.toggle('is-dirty', !!isDirty);
    },
  };
}

// ── Instance list ─────────────────────────────────────────────────────────────
// Flat array of { instance, name, icon, visible }.
// At most one entry has visible === true at any time.
const _instances = [];

function _currentVisible() {
  return _instances.find(e => e.visible) ?? null;
}

function _attachToBody(entry) {
  _bodyEl.innerHTML = '';
  _bodyEl.appendChild(entry.instance.el);
  _syncDecoration(entry);
}

// ── GritsClient singleton ─────────────────────────────────────────────────────
let _gritsClient = null;
function _getClient() {
  if (!_gritsClient) _gritsClient = new GritsClient();
  return _gritsClient;
}

// ── Shell singleton ───────────────────────────────────────────────────────────
// Shared across all eval() calls; widgets get their own shells internally.
let _shell = null;
function _getShell() {
  if (!_shell) {
    const evalContext = { fs: _getClient() };
    _shell = makeShell({
      fs:        _getClient(),
      serverUrl: window.location.origin,
      volume:    'client',
      cwd:       '/',
      libs:      [{ serverUrl: window.location.origin, volume: 'client', path: 'lib' }],
      evalContext,
    });
    evalContext.shell = _shell;
    // Expose singleton shell globally for console/debug use
    window.gsh = _shell;
  }
  return _shell;
}

// ── openWidget — the interface tools call via window.gimbal ───────────────────
// Matches the signature in gwm.html: openWidget(mod, opts).
// `zone` is accepted and silently ignored (no tiling in overlay mode).
async function openWidget(mod, opts = {}) {
  const { name = 'widget', icon = 'gterm', zone, evalContext: callerCtx, ...rest } = opts;

  _ensureDOM();

  // Minimize any currently visible widget
  const cur = _currentVisible();
  if (cur) cur.visible = false;

  // Build evalContext, always including the singleton GritsClient
  const evalContext = {
    fs:    _getClient(),
    shell: _getShell(),
    ...callerCtx,
  };

  // Instantiate the widget
  const createWidget = mod.default ?? mod;
  const instance = await Promise.resolve(
    createWidget({ name, evalContext, ...rest })
  );

  // Register entry before wiring controls so setTitle/setDirty can look it up
  const entry = { instance, name, icon, visible: true };
  _instances.push(entry);

  const controls = _makeControls(entry);
  instance.controls = controls;

  // Mount into overlay body and ensure overlay is in the DOM
  _attachToBody(entry);
  if (!document.getElementById('gimbal-overlay')) {
    document.body.appendChild(_overlayEl);
  }

  instance.focus?.();
}

// ── Destroy current visible widget ────────────────────────────────────────────
function _destroyCurrent() {
  const cur = _currentVisible();
  if (cur) {
    cur.instance.destroy?.();
    _instances.splice(_instances.indexOf(cur), 1);
  }
  if (_bodyEl) _bodyEl.innerHTML = '';
  _overlayEl?.remove();
}

// ── Public API ────────────────────────────────────────────────────────────────

/** Run a shell command through the overlay's shell, e.g. eval('gterm()'). */
export async function eval_(src) {
  return _getShell().eval(src, {}, { doHistory: true });
}

/** Hide the overlay without destroying widget state. */
export function hide() {
  _overlayEl?.remove();
}

/** Re-show the overlay, restoring whatever widget was last visible. */
export function show() {
  _ensureDOM();
  const cur = _currentVisible();
  if (!cur) return;
  _attachToBody(cur);
  if (!document.getElementById('gimbal-overlay')) {
    document.body.appendChild(_overlayEl);
  }
  cur.instance.focus?.();
}

/** Toggle overlay visibility. */
export function toggle() {
  if (document.getElementById('gimbal-overlay')) hide();
  else show();
}

// ── Install window.gimbal (same interface tools expect from gwm.html) ─────────
window.gimbal = { openWidget };

// ── Install window.gwm (convenience handle for console use) ──────────────────
window.gwm = {
  /**
   * Open a widget by running a shell command.
   * e.g.  await gwm.eval('gterm()')
   *       await gwm.eval('edit("lib/foo/main.js")')
   */
  eval: eval_,
  show,
  hide,
  toggle,
  /** Inspect the live instance list for debugging. */
  get instances() { return _instances; },
};

// Eagerly initialize shell so window.gsh is immediately available
_getShell();
