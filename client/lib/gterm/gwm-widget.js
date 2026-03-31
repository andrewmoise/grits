/*
 * @cell terminal-widget
 * @version 0.1
 * @about
 *   Gimbal shell terminal widget. A bare-bones REPL backed by GimbalShell.
 *   Supports command history, await expressions, and the full gsh tool
 *   dispatch pipeline.
 * @implements gimbal-shell#widget
 */

export default function createWidget({ name, evalContext = {} }) {
  const shell = evalContext.sh;

  // ── Root element ─────────────────────────────────────────
  const root = document.createElement('div');
  root.style.cssText = `
    display: flex; flex-direction: column;
    width: 100%; height: 100%;
    font-family: 'IBM Plex Mono', monospace;
    font-size: 12px;
    background: var(--bg);
    color: var(--text);
  `;

  // ── Output area ───────────────────────────────────────────
  const output = document.createElement('div');
  output.style.cssText = `
    flex: 1; overflow-y: auto; min-height: 0;
    padding: 8px 10px; display: flex; flex-direction: column; gap: 2px;
    user-select: text;
  `;

  // ── Input row ─────────────────────────────────────────────
  const inputRow = document.createElement('div');
  inputRow.style.cssText = `
    display: flex; align-items: center; gap: 6px;
    padding: 6px 10px;
    border-top: 1px solid var(--border);
    flex-shrink: 0;
  `;

  const promptLabel = document.createElement('span');
  promptLabel.style.cssText = `color: var(--accent); white-space: nowrap; flex-shrink: 0; font-size: 12px;`;

  const input = document.createElement('input');
  input.type = 'text';
  input.autocomplete = 'off';
  input.spellcheck = false;
  input.style.cssText = `
    flex: 1; background: transparent; border: none; outline: none;
    color: var(--text-hi); font-family: inherit; font-size: inherit;
    caret-color: var(--accent);
  `;
  input.placeholder = shell ? 'enter expression…' : 'no shell — set evalContext.sh';
  input.disabled = !shell;

  inputRow.appendChild(promptLabel);
  inputRow.appendChild(input);
  root.appendChild(output);
  root.appendChild(inputRow);

  // ── Location / prompt ─────────────────────────────────────
  function syncPrompt() {
    if (!shell) { promptLabel.textContent = 'gsh$ '; return; }
    const path   = shell.cwd || '/';
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
    promptLabel.textContent = loc.replace(/\/+/g, '/') + '$ ';
  }

  // ── Logging helpers ───────────────────────────────────────
  function _esc(s) {
    return String(s).replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;');
  }

  function logInfo(msg) {
    const d = document.createElement('div');
    d.style.cssText = 'color: var(--text-dim); font-style: italic;';
    d.textContent = msg;
    output.appendChild(d);
    output.scrollTop = output.scrollHeight;
  }

  function logCmd(loc, src, display, isError) {
    const entry = document.createElement('div');
    entry.style.cssText = 'display:flex;flex-direction:column;gap:2px;border-bottom:1px solid var(--bg3);padding-bottom:3px;';

    const prompt = document.createElement('div');
    prompt.innerHTML = `<span style="color:var(--accent)">${_esc(loc)}</span><span style="color:var(--text-dim)">$ </span><span style="color:var(--text-hi)">${_esc(src)}</span>`;
    entry.appendChild(prompt);

    const result = document.createElement('div');
    result.style.cssText = `padding-left:14px; white-space:pre-wrap; color:${
      isError ? 'var(--danger)' : display === null ? 'var(--text-dim)' : 'var(--text)'
    }`;
    result.textContent = display === null ? '(void)' : display;
    entry.appendChild(result);

    output.appendChild(entry);
    output.scrollTop = output.scrollHeight;
  }

  // ── Eval ──────────────────────────────────────────────────
  let historyIdx = -1;

  async function runExpr(src) {
    if (!shell || !src.trim()) return;
    const locAtTime = promptLabel.textContent.replace(/\$\s*$/, '').trim();
    try {
      const { display } = await shell.eval(src);
      logCmd(locAtTime, src, display, false);
    } catch(e) {
      logCmd(locAtTime, src, e.message ?? String(e), true);
      console.error(e);
    }
    syncPrompt();
  }

  // ── Input handling ────────────────────────────────────────
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
  });

  // ── Init ──────────────────────────────────────────────────
  syncPrompt();
  if (!shell) {
    logInfo('no shell available — pass evalContext.sh to activate');
  } else {
    logInfo(`${shell.volume ?? 'gsh'} ready`);
  }

  return {
    el: root,
    focus() { input.focus(); },
    destroy() {},
  };
}