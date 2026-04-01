/*
 * @cell terminal-widget
 * @version 0.8
 * @about
 *   Gimbal shell terminal widget. Classic inline-prompt layout.
 *   Single history array is the source of truth for all state.
 *   __ is a live array of result values, accessible in eval context.
 *   __[n] labels appear in the gutter on successful completion.
 */

import { VOID, isVoid, makeShell } from '../gimbal/gsh.js';

// ── cwd display label ─────────────────────────────────
function cwdLabel(shell) {
  const cwd = shell.cwd ?? '/';
  if (cwd === '/') return `:${shell.volume ?? 'client'}`;
  const parts = cwd.replace(/\/+$/, '').split('/');
  return parts[parts.length - 1];
}

// ── SVG icons ─────────────────────────────────────────
const SVG_SPINNER = `<svg class="gt-icon gt-spin" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="2">
  <circle cx="8" cy="8" r="6" stroke-opacity="0.2"/>
  <path d="M8 2 A6 6 0 0 1 14 8" stroke-linecap="round"/>
</svg>`;

const SVG_HOURGLASS = `<svg class="gt-icon" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.6">
  <path d="M4 2h8M4 14h8M5 2c0 3 3 4 3 6s-3 3-3 6M11 2c0 3-3 4-3 6s3 3 3 6" stroke-linecap="round" stroke-linejoin="round"/>
</svg>`;

const SVG_ERROR = `<svg class="gt-icon" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.6">
  <circle cx="8" cy="8" r="6"/>
  <line x1="8" y1="5" x2="8" y2="8.5" stroke-linecap="round"/>
  <circle cx="8" cy="11" r="0.75" fill="currentColor" stroke="none"/>
</svg>`;

export default function createWidget({ name, evalContext = {}, runOnInit = null }) {
  const shell = makeShell({
    gg:        evalContext.fs,
    serverUrl: window.location.origin,
    volume:    'client',
    cwd:       '/',
    libs:      [{ serverUrl: window.location.origin, volume: 'client', path: 'lib' }],
    evalContext,
  });

  // ── result history — live array passed into every eval ────────
  // __[0] is the first result, __[n] the nth. Void results are
  // stored as undefined so indices stay stable.
  const __ = [];

  // ── root element ──────────────────────────────────────
  const el = document.createElement('div');
  el.style.cssText = 'width:100%;height:100%;display:flex;flex-direction:column;overflow:hidden;';

  // ── inject scoped styles once ─────────────────────────
  const STYLE_ID = 'gimbal-terminal-styles';
  if (!document.getElementById(STYLE_ID)) {
    const s = document.createElement('style');
    s.id = STYLE_ID;
    s.textContent = `
      .gt-output {
        flex: 1;
        overflow-y: auto;
        overflow-x: hidden;
        padding: 0.625rem 0.75rem 0.375rem;
        font-family: 'JetBrains Mono', 'IBM Plex Mono', 'Fira Mono', monospace;
        font-size: 0.75rem;
        line-height: 1.6;
        word-break: break-all;
        user-select: text;
        cursor: text;
        display: flex;
        flex-direction: column;
      }
      .gt-output::-webkit-scrollbar { width: 0.25rem; }
      .gt-output::-webkit-scrollbar-thumb {
        background: var(--border-hi); border-radius: 0.125rem;
      }

      .gt-entry {
        display: flex;
        flex-direction: column;
        gap: 0.125rem;
        margin-bottom: 0.125rem;
      }
      .gt-cmd-line {
        display: flex; align-items: baseline; gap: 0.375rem;
      }
      .gt-loc  { color: var(--a1); flex-shrink: 0; white-space: nowrap; }
      .gt-sep  { color: var(--text-dim); flex-shrink: 0; }
      .gt-src  { color: var(--text-hi); flex: 1; white-space: pre-wrap; }
      .gt-cmd-line.is-queued .gt-src { color: var(--text-dim); }

      .gt-status {
        display: flex; align-items: center;
        flex-shrink: 0; width: 1.1rem; height: 1.1rem;
        position: relative; top: 0.1em;
        margin-left: auto;
      }
      .gt-icon { width: 0.85rem; height: 0.85rem; }
      .gt-icon.gt-spin {
        animation: gt-spin 0.9s linear infinite;
        color: var(--a1);
      }
      @keyframes gt-spin {
        from { transform: rotate(0deg); }
        to   { transform: rotate(360deg); }
      }
      .gt-status.is-queued { color: var(--text-dim); }
      .gt-status.is-error  { color: var(--red); }
      .gt-status.is-ref {
        font-family: 'JetBrains Mono', 'IBM Plex Mono', monospace;
        font-size: 0.6rem;
        color: var(--text-dim);
        width: auto;
        letter-spacing: -0.02em;
      }

      .gt-result {
        padding-left: 0.875rem;
        white-space: pre-wrap;
        color: var(--text);
        margin-bottom: 0.25rem;
      }
      .gt-result.is-error { color: var(--red); }

      .gt-input-line {
        display: flex;
        align-items: flex-start;
        gap: 0.375rem;
        margin-top: 0.125rem;
        padding-bottom: 0.375rem;
      }
      .gt-input-loc {
        color: var(--a1);
        white-space: nowrap;
        flex-shrink: 0;
        line-height: 1.6;
      }
      .gt-input-sep {
        color: var(--text-dim);
        flex-shrink: 0;
        line-height: 1.6;
      }
      .gt-textarea {
        flex: 1;
        background: transparent;
        border: none;
        outline: none;
        resize: none;
        overflow: hidden;
        color: var(--text-hi);
        font-family: 'JetBrains Mono', 'IBM Plex Mono', monospace;
        font-size: 0.75rem;
        line-height: 1.6;
        caret-color: var(--a1);
        min-width: 0;
        padding: 0;
        margin: 0;
        height: 1.2em;
      }
      .gt-textarea::placeholder { color: var(--text-dim); opacity: 0.5; }
    `;
    document.head.appendChild(s);
  }

  // ── history — single source of truth ──────────────────
  const history = [];
  let running = false;

  // ── output area ───────────────────────────────────────
  const output = document.createElement('div');
  output.className = 'gt-output';

  const spacer = document.createElement('div');
  spacer.style.cssText = 'flex: 1 1 auto; min-height: 0;';
  output.appendChild(spacer);

  el.appendChild(output);

  // ── live input line ───────────────────────────────────
  const inputLine = document.createElement('div');
  inputLine.className = 'gt-input-line';

  const inputLoc = document.createElement('span');
  inputLoc.className = 'gt-input-loc';

  const inputSep = document.createElement('span');
  inputSep.className = 'gt-input-sep';
  inputSep.textContent = '$';

  const textarea = document.createElement('textarea');
  textarea.className = 'gt-textarea';
  textarea.rows = 1;
  textarea.autocomplete = 'off';
  textarea.spellcheck = false;
  textarea.placeholder = '';

  inputLine.appendChild(inputLoc);
  inputLine.appendChild(inputSep);
  inputLine.appendChild(textarea);
  output.appendChild(inputLine);

  function resizeTextarea() {
    textarea.style.height = '0';
    textarea.style.height = textarea.scrollHeight + 'px';
  }
  textarea.addEventListener('input', resizeTextarea);

  // ── DOM builders ──────────────────────────────────────
  function buildEntryDOM(rec) {
    const entry = document.createElement('div');
    entry.className = 'gt-entry';

    const cmdLine = document.createElement('div');
    cmdLine.className = 'gt-cmd-line is-queued';

    const srcEl = document.createElement('span');
    srcEl.className = 'gt-src';
    srcEl.textContent = rec.src;

    const statusEl = document.createElement('span');
    statusEl.className = 'gt-status is-queued';
    statusEl.innerHTML = SVG_HOURGLASS;

    cmdLine.appendChild(srcEl);
    cmdLine.appendChild(statusEl);
    entry.appendChild(cmdLine);

    output.insertBefore(entry, inputLine);
    output.scrollTop = output.scrollHeight;

    rec.dom = { entry, cmdLine, srcEl, statusEl, locEl: null, spinnerTimer: null };
  }

  function applyRunning(rec) {
    const { cmdLine, srcEl, statusEl } = rec.dom;
    cmdLine.classList.remove('is-queued');

    const locEl = document.createElement('span');
    locEl.className = 'gt-loc';
    locEl.textContent = cwdLabel(shell);
    rec.dom.locEl = locEl;

    const sep = document.createElement('span');
    sep.className = 'gt-sep';
    sep.textContent = '$';

    cmdLine.insertBefore(sep, srcEl);
    cmdLine.insertBefore(locEl, sep);

    statusEl.className = 'gt-status';
    statusEl.innerHTML = '';
    rec.dom.spinnerTimer = setTimeout(() => {
      statusEl.innerHTML = SVG_SPINNER;
    }, 200);
  }

  function applyDone(rec) {
    const { statusEl, entry } = rec.dom;
    clearTimeout(rec.dom.spinnerTimer);

    const isError = rec.status === 'error';

    if (isError) {
      statusEl.className = 'gt-status is-error';
      statusEl.innerHTML = SVG_ERROR;
    } else if (rec.refIndex !== null) {
      statusEl.className = 'gt-status is-ref';
      statusEl.innerHTML = '';
      statusEl.textContent = `__[${rec.refIndex}]`;
    } else {
      // void result — no icon, no label
      statusEl.className = 'gt-status';
      statusEl.innerHTML = '';
    }

    if (rec.display !== null) {
      const result = document.createElement('div');
      result.className = `gt-result${isError ? ' is-error' : ''}`;
      result.textContent = rec.display;
      entry.appendChild(result);
      output.scrollTop = output.scrollHeight;
    }
  }

  // ── execution loop ────────────────────────────────────
  async function runNext() {
    if (running) return;
    const rec = history.find(r => r.status === 'queued');
    if (!rec) {
      inputLoc.textContent = cwdLabel(shell);
      return;
    }

    running = true;
    rec.status = 'running';
    applyRunning(rec);
    inputLoc.textContent = '';

    try {
      const { value, display } = await shell.eval(rec.src, 80, { __, _: __.length ? __[__.length - 1] : VOID });
      rec.status   = 'done';
      rec.display  = display;

      if (!isVoid(value)) {
        rec.refIndex = __.length;
        __.push(value);
      } else {
        rec.refIndex = null;
      }
    } catch(e) {
      rec.status   = 'error';
      rec.display  = e.message ?? String(e);
      rec.refIndex = null;
      console.error(e);
    }

    applyDone(rec);
    inputLoc.textContent = cwdLabel(shell);
    running = false;
    runNext();
  }

  // ── enqueue ───────────────────────────────────────────
  function enqueue(src) {
    const rec = {
      src,
      status:   'queued',
      display:  null,
      refIndex: null,
      dom:      null,
    };
    history.push(rec);
    buildEntryDOM(rec);
    inputLoc.textContent = '';
    runNext();
  }

  // ── keyboard handling ─────────────────────────────────
  let historyIdx = -1;

  textarea.addEventListener('keydown', e => {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault();
      const src = textarea.value.trim();
      if (!src) return;
      textarea.value = '';
      resizeTextarea();
      historyIdx = -1;
      enqueue(src);
      return;
    }

    const h = shell.history;
    if (e.key === 'ArrowUp') {
      const beforeCursor = textarea.value.slice(0, textarea.selectionStart);
      if (beforeCursor.includes('\n')) return;
      if (!h.length) return;
      e.preventDefault();
      historyIdx = Math.min(historyIdx + 1, h.length - 1);
      textarea.value = h[h.length - 1 - historyIdx];
      resizeTextarea();
    }
    if (e.key === 'ArrowDown') {
      const afterCursor = textarea.value.slice(textarea.selectionEnd);
      if (afterCursor.includes('\n')) return;
      e.preventDefault();
      if (historyIdx <= 0) { historyIdx = -1; textarea.value = ''; resizeTextarea(); return; }
      historyIdx--;
      textarea.value = h[h.length - 1 - historyIdx];
      resizeTextarea();
    }
    if (e.key === 'l' && e.ctrlKey) {
      e.preventDefault();
      output.innerHTML = '';
      output.appendChild(spacer);
      output.appendChild(inputLine);
      inputLoc.textContent = cwdLabel(shell);
    }
  });

  output.addEventListener('click', () => {
    if (!window.getSelection().toString()) textarea.focus();
  });

  // ── init ──────────────────────────────────────────────
  shell._warmCache().then(() => {
    inputLoc.textContent = cwdLabel(shell);
    if (runOnInit) enqueue(runOnInit);
  });
  textarea.focus();

  return {
    el,
    focus()   { textarea.focus(); },
    destroy() {},
  };
}