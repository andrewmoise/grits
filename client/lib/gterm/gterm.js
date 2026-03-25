/*
 * @cell terminal-widget
 * @version 0.3
 * @about
 *   Interactive JS console for the Gimbal shell. No external dependencies —
 *   output is a scrolling div with span-based coloring, input is a plain
 *   text input with readline-style history. Evals in a context containing
 *   whatever is passed as evalContext ({ ns, sh, etc. }). Promises are
 *   awaited automatically. Return values are pretty-printed.
 * @implements gimbal-shell#widget
 */

function prettyPrint(val, depth = 0) {
  const indent  = '  '.repeat(depth);
  const indent1 = '  '.repeat(depth + 1);

  if (val === undefined) return span('undefined', 'clr-dim');
  if (val === null)      return span('null',      'clr-dim');
  if (typeof val === 'string')   return span(`"${val}"`,    'clr-green');
  if (typeof val === 'number')   return span(String(val),   'clr-yellow');
  if (typeof val === 'boolean')  return span(String(val),   'clr-purple');
  if (typeof val === 'function') return span(`[Function: ${val.name || 'anonymous'}]`, 'clr-dim');

  if (Array.isArray(val)) {
    if (val.length === 0) return span('[]', 'clr-dim');
    if (depth >= 2) return span(`[Array(${val.length})]`, 'clr-dim');
    const items = val.map(v => indent1 + prettyPrint(v, depth + 1));
    return `[\n${items.join(',\n')}\n${indent}]`;
  }

  if (typeof val === 'object') {
    const keys = Object.keys(val);
    if (keys.length === 0) return span('{}', 'clr-dim');
    if (depth >= 2) return span(`{${keys.slice(0,3).join(', ')}${keys.length > 3 ? '…' : ''}}`, 'clr-dim');
    const items = keys.map(k => `${indent1}${span(k, 'clr-cyan')}: ${prettyPrint(val[k], depth + 1)}`);
    return `{\n${items.join(',\n')}\n${indent}}`;
  }

  return String(val);
}

function span(text, cls) {
  return `<span class="${cls}">${escHtml(text)}</span>`;
}

function escHtml(s) {
  return String(s)
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;');
}

export default function createWidget({ name, evalContext = {} }) {
  // ── root element ─────────────────────────────────────
  const el = document.createElement('div');
  el.style.cssText = 'width:100%;height:100%;display:flex;flex-direction:column;overflow:hidden;';

  // ── inject scoped styles once ─────────────────────────
  const STYLE_ID = 'gimbal-console-styles';
  if (!document.getElementById(STYLE_ID)) {
    const s = document.createElement('style');
    s.id = STYLE_ID;
    s.textContent = `
      .gc-output {
        flex: 1;
        overflow-y: auto;
        overflow-x: hidden;
        padding: 10px 12px 4px;
        font-family: "IBM Plex Mono", "Fira Mono", monospace;
        font-size: 12px;
        line-height: 1.6;
        word-break: break-all;
      }
      .gc-output::-webkit-scrollbar { width: 4px; }
      .gc-output::-webkit-scrollbar-thumb { background: var(--border-hi); border-radius: 2px; }

      .gc-row { display: flex; gap: 8px; padding: 1px 0; }
      .gc-row + .gc-row { border-top: none; }

      .gc-gutter {
        flex-shrink: 0;
        width: 16px;
        text-align: right;
        color: var(--text-dim);
        opacity: 0.4;
        font-size: 10px;
        padding-top: 2px;
        user-select: none;
      }
      .gc-gutter.in  { color: var(--accent); opacity: 0.7; }
      .gc-gutter.err { color: var(--danger);  opacity: 0.9; }

      .gc-text {
        flex: 1;
        white-space: pre-wrap;
        min-width: 0;
      }
      .gc-text.input-echo { color: var(--text-hi); }
      .gc-text.error      { color: var(--danger); }
      .gc-text.output     { color: var(--text); }

      .clr-dim    { color: var(--text-dim); }
      .clr-green  { color: #7ec8a0; }
      .clr-yellow { color: #d4b866; }
      .clr-purple { color: #a07ec8; }
      .clr-cyan   { color: #5bc8c8; }

      .gc-input-row {
        display: flex;
        align-items: center;
        gap: 6px;
        padding: 6px 12px 8px;
        border-top: 1px solid var(--border);
        flex-shrink: 0;
      }
      .gc-prompt-sym {
        color: var(--accent);
        font-family: "IBM Plex Mono", monospace;
        font-size: 13px;
        flex-shrink: 0;
        user-select: none;
      }
      .gc-input {
        flex: 1;
        background: transparent;
        border: none;
        outline: none;
        color: var(--text-hi);
        font-family: "IBM Plex Mono", "Fira Mono", monospace;
        font-size: 12px;
        caret-color: var(--accent);
        min-width: 0;
      }
      .gc-input::placeholder { color: var(--text-dim); opacity: 0.5; }
    `;
    document.head.appendChild(s);
  }

  // ── output area ───────────────────────────────────────
  const output = document.createElement('div');
  output.className = 'gc-output';

  // ── input row ─────────────────────────────────────────
  const inputRow = document.createElement('div');
  inputRow.className = 'gc-input-row';

  const promptSym = document.createElement('span');
  promptSym.className = 'gc-prompt-sym';
  promptSym.textContent = '›';

  const input = document.createElement('input');
  input.className = 'gc-input';
  input.type = 'text';
  input.autocomplete = 'off';
  input.spellcheck = false;
  input.placeholder = 'javascript…';

  inputRow.appendChild(promptSym);
  inputRow.appendChild(input);
  el.appendChild(output);
  el.appendChild(inputRow);

  // ── output helpers ────────────────────────────────────
  function appendRow(gutterText, gutterCls, textHTML, textCls) {
    const row = document.createElement('div');
    row.className = 'gc-row';

    const gutter = document.createElement('span');
    gutter.className = `gc-gutter ${gutterCls}`;
    gutter.textContent = gutterText;

    const text = document.createElement('span');
    text.className = `gc-text ${textCls}`;
    text.innerHTML = textHTML;

    row.appendChild(gutter);
    row.appendChild(text);
    output.appendChild(row);
    output.scrollTop = output.scrollHeight;
  }

  function appendInput(code) {
    appendRow('›', 'in', escHtml(code), 'input-echo');
  }

  function appendOutput(val) {
    appendRow('←', '', prettyPrint(val), 'output');
  }

  function appendError(msg) {
    appendRow('✖', 'err', escHtml(msg), 'error');
  }

  function appendInfo(html) {
    appendRow('', '', html, 'output clr-dim');
  }

  // ── eval context ──────────────────────────────────────
  const ctx = {
    ...evalContext,
    clear: () => { output.innerHTML = ''; return undefined; },
    help:  () => {
      appendInfo([
        '<span style="color:var(--text-hi);font-weight:500">Gimbal JS Console</span>',
        'Plain JavaScript. Objects in scope:',
        '  <span class="clr-cyan">ns</span>      — namespace / filesystem',
        '  <span class="clr-cyan">sh</span>      — shell commands',
        '  <span class="clr-cyan">clear()</span> — clear output',
        '  <span class="clr-cyan">help()</span>  — this message',
      ].join('\n'));
      return undefined;
    },
  };

  function evalInContext(code) {
    const keys = Object.keys(ctx);
    const vals = Object.values(ctx);
    // eslint-disable-next-line no-new-func
    return new Function(...keys, `"use strict"; return (${code})`)(...vals);
  }

  // ── history ───────────────────────────────────────────
  const history = [];
  let histIdx   = -1;
  let savedLine = '';

  // ── submit ────────────────────────────────────────────
  async function submit() {
    const code = input.value.trim();
    input.value = '';
    histIdx = -1; savedLine = '';

    if (!code) return;
    if (history[0] !== code) history.unshift(code);
    if (history.length > 200) history.pop();

    appendInput(code);

    try {
      let result = evalInContext(code);
      if (result instanceof Promise) {
        const pending = document.createElement('div');
        pending.className = 'gc-row';
        pending.innerHTML = `<span class="gc-gutter clr-dim">…</span><span class="gc-text clr-dim">awaiting…</span>`;
        output.appendChild(pending);
        output.scrollTop = output.scrollHeight;
        result = await result;
        output.removeChild(pending);
      }
      if (result !== undefined) appendOutput(result);
    } catch (err) {
      appendError(err.message ?? String(err));
    }
  }

  // ── keyboard ──────────────────────────────────────────
  input.addEventListener('keydown', e => {
    if (e.key === 'Enter') {
      e.preventDefault(); submit(); return;
    }
    if (e.key === 'ArrowUp') {
      e.preventDefault();
      if (!history.length) return;
      if (histIdx === -1) savedLine = input.value;
      histIdx = Math.min(histIdx + 1, history.length - 1);
      input.value = history[histIdx];
      // move cursor to end
      requestAnimationFrame(() => input.setSelectionRange(input.value.length, input.value.length));
      return;
    }
    if (e.key === 'ArrowDown') {
      e.preventDefault();
      if (histIdx === -1) return;
      histIdx--;
      input.value = histIdx === -1 ? savedLine : history[histIdx];
      requestAnimationFrame(() => input.setSelectionRange(input.value.length, input.value.length));
      return;
    }
    if (e.key === 'l' && e.ctrlKey) {
      e.preventDefault(); output.innerHTML = ''; return;
    }
  });

  // ── welcome ───────────────────────────────────────────
  appendInfo('<span style="color:var(--text-hi);font-weight:500">Gimbal</span> JS console — <span class="clr-cyan">help()</span> for info');

  // ── click anywhere in output focuses input ────────────
  output.addEventListener('click', () => input.focus());

  return {
    el,
    focus()   { input.focus(); },
    destroy() {},
  };
}

