/*
 * @cell terminal-widget
 * @version 0.4
 * @about
 *   Gimbal shell terminal widget. A bare-bones REPL backed by GimbalShell.
 *   Supports command history, await expressions, and the full gsh tool
 *   dispatch pipeline. Input row hides while computing; void results are
 *   silent; text in output area is selectable.
 * @implements gimbal-shell#widget
 */

import { isVoid } from '../gimbal/gsh.js';

function escHtml(s) {
  return String(s)
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;');
}

export default function createWidget({ name, evalContext = {} }) {
  const shell = evalContext.sh;

  // ── root element ─────────────────────────────────────
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
        display: flex;          /* ← add */
        flex-direction: column; /* ← add */
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
      .gt-loc  { color: var(--a1); white-space: nowrap; }
      .gt-sep  { color: var(--text-dim); }
      .gt-src  { color: var(--text-hi); }

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
      .gt-input:disabled { opacity: 0.4; }
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

  const input = document.createElement('input');
  input.className = 'gt-input';
  input.type = 'text';
  input.autocomplete = 'off';
  input.spellcheck = false;
  input.placeholder = shell ? 'enter expression…' : 'no shell — set evalContext.sh';
  input.disabled = !shell;

  inputRow.appendChild(promptEl);
  inputRow.appendChild(input);
  el.appendChild(output);
  el.appendChild(inputRow);

  // ── prompt / location ─────────────────────────────────
  function syncPrompt() {
    if (!shell) { promptEl.textContent = 'gsh$ '; return; }
    const volume = shell.volume ?? '';
    let hostPart = '';
    if (shell.serverUrl) {
      const defaultHost = window.location.hostname;
      const shellHost   = shell.serverUrl.replace(/^https?:\/\//, '').replace(/\/$/, '');
      if (shellHost !== defaultHost) hostPart = shellHost;
    }
    const loc = hostPart
      ? `${hostPart}:${volume}${shell.cwd}`
      : `:${volume}${shell.cwd}`;
    promptEl.textContent = loc.replace(/\/+/g, '/') + '$ ';
  }

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

  function logCmd(loc, src, display, isError) {
    const entry = document.createElement('div');
    entry.className = 'gt-entry';

    const pline = document.createElement('div');
    pline.className = 'gt-prompt-line';
    pline.innerHTML = `<span class="gt-loc">${escHtml(loc)}</span><span class="gt-sep">$</span><span class="gt-src">${escHtml(src)}</span>`;
    entry.appendChild(pline);

    // Only add a result line if there's something to show
    if (display !== null || isError) {
      const result = document.createElement('div');
      result.className = `gt-result${isError ? ' is-error' : ''}`;
      result.textContent = display ?? '';
      entry.appendChild(result);
    }

    output.appendChild(entry);
    output.scrollTop = output.scrollHeight;
  }

  // ── eval ──────────────────────────────────────────────
  let historyIdx = -1;
  let busy = false;

  async function runExpr(src) {
    if (!shell || !src.trim() || busy) return;
    busy = true;
    const locAtTime = promptEl.textContent.replace(/\$\s*$/, '').trim();

    // Hide input row while computing
    inputRow.style.display = 'none';

    try {
      const { display } = await shell.eval(src);
      logCmd(locAtTime, src, display, false);
    } catch(e) {
      logCmd(locAtTime, src, e.message ?? String(e), true);
      console.error(e);
    }

    // Restore input row
    inputRow.style.display = '';
    busy = false;
    syncPrompt();
    input.focus();
  }

  // ── input handling ────────────────────────────────────
  input.addEventListener('keydown', e => {
    if (e.key === 'Enter') {
      const src = input.value.trim();
      if (!src) return;
      input.value = ''; historyIdx = -1;
      runExpr(src);
      return;
    }
    if (!shell) return;
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
      e.preventDefault(); output.innerHTML = '';
    }
  });

  // ── click to focus (without breaking text selection) ──
  output.addEventListener('click', () => {
    if (!window.getSelection().toString()) input.focus();
  });

  // ── init ──────────────────────────────────────────────
  syncPrompt();
  if (!shell) {
    logInfo('no shell — pass evalContext.sh to activate');
  } else {
    logInfo(`${shell.volume ?? 'gimbal'} ready`);
  }

  return {
    el,
    focus()   { input.focus(); },
    destroy() {},
  };
}