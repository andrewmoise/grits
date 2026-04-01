/*
 * @cell terminal-widget
 * @version 0.5
 * @about
 *   Gimbal shell terminal widget. REPL backed by GimbalShell.
 *   Supports command history, queued execution, per-entry status icons,
 *   and the full gsh tool dispatch pipeline.
 */

import { isVoid, makeShell } from '../gimbal/gsh.js';

function escHtml(s) {
  return String(s)
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;');
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

export default function createWidget({ name, evalContext = {} }) {
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
        padding: 0.625rem 0.75rem 0.25rem;
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
        border-bottom: 1px solid var(--border);
        padding-bottom: 0.1875rem;
        margin-bottom: 0.125rem;
      }
      .gt-entry:last-child { border-bottom: none; }

      .gt-prompt-line {
        display: flex; align-items: baseline; gap: 0.375rem;
      }
      .gt-sep  { color: var(--text-dim); flex-shrink: 0; }
      .gt-src  { color: var(--text-hi); flex: 1; }

      .gt-status {
        display: flex; align-items: center;
        flex-shrink: 0; width: 1rem; height: 1rem;
        position: relative; top: 0.1em;
      }
      .gt-status:empty { display: none; }

      .gt-icon {
        width: 0.85rem; height: 0.85rem;
      }
      .gt-icon.gt-spin {
        animation: gt-spin 0.9s linear infinite;
        color: var(--a1);
      }
      @keyframes gt-spin {
        from { transform: rotate(0deg); }
        to   { transform: rotate(360deg); }
      }
      .gt-status.is-queued  { color: var(--text-dim); }
      .gt-status.is-error   { color: var(--red); }

      .gt-result {
        padding-left: 0.875rem;
        white-space: pre-wrap;
        color: var(--text);
      }
      .gt-result.is-error { color: var(--red); }
      .gt-result.is-info  { color: var(--text-dim); font-style: italic; }

      .gt-input-row {
        display: flex;
        align-items: center;
        gap: 0.375rem;
        padding: 0.375rem 0.625rem 0.5rem;
        border-top: 1px solid var(--border);
        flex-shrink: 0;
      }
      .gt-prompt {
        color: var(--a1);
        font-family: 'JetBrains Mono', 'IBM Plex Mono', monospace;
        font-size: 0.75rem;
        flex-shrink: 0;
        white-space: nowrap;
        user-select: none;
      }
      .gt-input {
        flex: 1;
        background: transparent;
        border: none;
        outline: none;
        color: var(--text-hi);
        font-family: 'JetBrains Mono', 'IBM Plex Mono', monospace;
        font-size: 0.75rem;
        caret-color: var(--a1);
        min-width: 0;
      }
      .gt-input::placeholder { color: var(--text-dim); opacity: 0.5; }
    `;
    document.head.appendChild(s);
  }

  // ── output area ───────────────────────────────────────
  const output = document.createElement('div');
  output.className = 'gt-output';

  const spacer = document.createElement('div');
  spacer.style.cssText = 'flex: 1 1 auto; min-height: 0;';
  output.appendChild(spacer);

  // ── input row ─────────────────────────────────────────
  const inputRow = document.createElement('div');
  inputRow.className = 'gt-input-row';

  const promptEl = document.createElement('span');
  promptEl.className = 'gt-prompt';
  promptEl.textContent = '$';

  const input = document.createElement('input');
  input.className = 'gt-input';
  input.type = 'text';
  input.autocomplete = 'off';
  input.spellcheck = false;
  input.placeholder = 'enter expression…';

  inputRow.appendChild(promptEl);
  inputRow.appendChild(input);
  el.appendChild(output);
  el.appendChild(inputRow);

  // ── output helpers ────────────────────────────────────
  function logInfo(msg) {
    const entry = document.createElement('div');
    entry.className = 'gt-entry';
    const r = document.createElement('div');
    r.className = 'gt-result is-info';
    r.textContent = msg;
    entry.appendChild(r);
    output.appendChild(entry);
    output.scrollTop = output.scrollHeight;
  }

  // Creates an entry DOM node and returns handles to update it
  function createEntry(src) {
    const entry = document.createElement('div');
    entry.className = 'gt-entry';

    const pline = document.createElement('div');
    pline.className = 'gt-prompt-line';

    const sep = document.createElement('span');
    sep.className = 'gt-sep';
    sep.textContent = '$';

    const srcEl = document.createElement('span');
    srcEl.className = 'gt-src';
    srcEl.textContent = src;

    const statusEl = document.createElement('span');
    statusEl.className = 'gt-status';

    pline.appendChild(sep);
    pline.appendChild(srcEl);
    pline.appendChild(statusEl);
    entry.appendChild(pline);

    output.appendChild(entry);
    output.scrollTop = output.scrollHeight;

    let spinnerTimer = null;

    return {
      // Call when queued (not yet started)
      setQueued() {
        statusEl.className = 'gt-status is-queued';
        statusEl.innerHTML = SVG_HOURGLASS;
      },
      // Call when execution begins
      setRunning() {
        statusEl.className = 'gt-status';
        statusEl.innerHTML = '';
        // Only show spinner if it takes more than 200ms
        spinnerTimer = setTimeout(() => {
          statusEl.innerHTML = SVG_SPINNER;
        }, 350);
      },
      // Call on completion
      setDone(display, isError) {
        clearTimeout(spinnerTimer);
        statusEl.className = isError ? 'gt-status is-error' : 'gt-status';
        statusEl.innerHTML = isError ? SVG_ERROR : '';

        if (display !== null) {
          const result = document.createElement('div');
          result.className = `gt-result${isError ? ' is-error' : ''}`;
          result.textContent = display;
          entry.appendChild(result);
          output.scrollTop = output.scrollHeight;
        }
      },
    };
  }

  // ── queue & execution ─────────────────────────────────
  // Each item: { src, entryHandle }
  const queue = [];
  let busy = false;

  async function runNext() {
    if (busy || queue.length === 0) return;
    busy = true;

    const { src, entryHandle } = queue.shift();
    entryHandle.setRunning();

    try {
      const { display } = await shell.eval(src);
      entryHandle.setDone(display, false);
    } catch(e) {
      entryHandle.setDone(e.message ?? String(e), true);
      console.error(e);
    }

    busy = false;
    runNext();
  }

  function enqueue(src) {
    const entryHandle = createEntry(src);
    const isFirst = queue.length === 0 && !busy;
    queue.push({ src, entryHandle });
    if (isFirst) {
      runNext();
    } else {
      entryHandle.setQueued();
    }
  }

  // ── input handling ────────────────────────────────────
  let historyIdx = -1;

  input.addEventListener('keydown', e => {
    if (e.key === 'Enter') {
      const src = input.value.trim();
      if (!src) return;
      input.value = '';
      historyIdx = -1;
      enqueue(src);
      return;
    }
    const h = shell.history;
    if (e.key === 'ArrowUp') {
      if (!h.length) return;
      historyIdx = Math.min(historyIdx + 1, h.length - 1);
      input.value = h[h.length - 1 - historyIdx];
      e.preventDefault();
    }
    if (e.key === 'ArrowDown') {
      if (historyIdx <= 0) { historyIdx = -1; input.value = ''; return; }
      historyIdx--;
      input.value = h[h.length - 1 - historyIdx];
      e.preventDefault();
    }
    if (e.key === 'l' && e.ctrlKey) {
      e.preventDefault();
      // Clear output but keep spacer
      output.innerHTML = '';
      output.appendChild(spacer);
    }
  });

  // ── click to focus (without breaking text selection) ──
  output.addEventListener('click', () => {
    if (!window.getSelection().toString()) input.focus();
  });

  // ── init ──────────────────────────────────────────────
  logInfo(`${shell.volume ?? 'gimbal'} ready`);
  input.focus();

  return {
    el,
    focus()   { input.focus(); },
    destroy() {},
  };
}