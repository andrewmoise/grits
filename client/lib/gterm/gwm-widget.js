/*
 * @cell terminal-widget
 * @version 0.7
 * @about
 *   Gimbal shell terminal widget. Classic inline-prompt layout.
 *   Single history array is the source of truth for all state.
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
  //
  // Each record:
  //   src        : string  — the command text
  //   status     : 'queued' | 'running' | 'done' | 'error'
  //   display    : string | null — result text (null until done)
  //   dom        : { entry, locEl, statusEl, resultContainer } — live DOM refs
  //
  const history = [];
  let running = false; // true while a command is executing

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
  textarea.placeholder = 'enter expression…';

  inputLine.appendChild(inputLoc);
  inputLine.appendChild(inputSep);
  inputLine.appendChild(textarea);
  output.appendChild(inputLine);

  // ── prompt sync ───────────────────────────────────────
  // The live input prompt shows the current directory only when idle.
  // When anything is running or queued we don't know where we'll land.
  function syncInputPrompt() {
    inputLoc.textContent = running || history.some(r => r.status === 'queued' || r.status === 'running')
      ? ''
      : cwdLabel(shell);
  }
  syncInputPrompt();

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

    // locEl and sep are inserted when command starts running
    rec.dom = { entry, cmdLine, srcEl, statusEl, locEl: null, spinnerTimer: null };
  }

  function applyRunning(rec) {
    const { cmdLine, srcEl, statusEl } = rec.dom;
    cmdLine.classList.remove('is-queued');

    const locEl = document.createElement('span');
    locEl.className = 'gt-loc';
    locEl.textContent = cwdLabel(shell);
    rec.dom.locEl = locEl;  // ← store it

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
    statusEl.className = isError ? 'gt-status is-error' : 'gt-status';
    statusEl.innerHTML = isError ? SVG_ERROR : '';

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
      syncInputPrompt(); // queue fully drained — show directory
      return;
    }

    running = true;
    rec.status = 'running';
    applyRunning(rec);
    syncInputPrompt(); // still busy, clears the directory from input prompt

    try {
      const { display } = await shell.eval(rec.src);
      rec.status  = 'done';
      rec.display = display;
    } catch(e) {
      rec.status  = 'error';
      rec.display = e.message ?? String(e);
      console.error(e);
    }

    applyDone(rec);
    running = false;
    runNext();
  }

  // ── enqueue ───────────────────────────────────────────
  function enqueue(src) {
    const rec = {
      src,
      status:    'queued',
      display:   null,
      dom:       null,
    };
    history.push(rec);
    buildEntryDOM(rec);
    syncInputPrompt();
    runNext();
  }

  // ── keyboard handling ─────────────────────────────────
  let historyIdx = -1;

  textarea.addEventListener('keydown', e => {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault();
      const src = textarea.value.trim();
      if (!src) return;
      inputLoc.textContent = '';
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
      syncInputPrompt();
    }
  });

  output.addEventListener('click', () => {
    if (!window.getSelection().toString()) textarea.focus();
  });

  // ── init ──────────────────────────────────────────────
  shell._warmCache().then(() => {
    syncInputPrompt();
    if (runOnInit) enqueue(runOnInit);
  });
  textarea.focus();

  return {
    el,
    focus()   { textarea.focus(); },
    destroy() {},
  };
}