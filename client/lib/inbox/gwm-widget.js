/*
 * @cell inbox-widget  v0.1
 * @about
 *   Inbox reader for the Gimbal shell. Lists messages dropped into
 *   the user's local/inbox directory. Each row shows From and Subject;
 *   expand to read the full message. Trash button deletes messages.
 * @implements gimbal-shell#widget
 */

import { FONT_MONO, injectStyles } from '../style/style.js';
import { toast } from '../gimbal/dialog.js';
import { ASSERT_IS_BLOB } from '../grits/GritsClient.js';

const STYLE_ID = 'gimbal-inbox-styles';

const SVG_CARET = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor"
  stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"
  style="width:1.2em;height:1.2em;display:block;flex-shrink:0;
         transition:transform calc(var(--dur) * 0.5) var(--ease-sine);
         transform-origin:center;">
  <polyline points="9 18 15 12 9 6"/>
</svg>`;

function ensureStyles() {
  injectStyles(STYLE_ID, `
    .gi-tree {
      padding: 0.375rem 0;
      font-family: ${FONT_MONO};
      font-size: var(--fs-base, 0.75rem);
      line-height: 1.5;
      user-select: none;
      overflow-y: auto;
    }

    .gi-row {
      display: flex;
      align-items: center;
      gap: 0.25rem;
      padding: 0.2rem 0.5rem;
      cursor: pointer;
      border-radius: 0.3rem;
      color: var(--text);
      transition: background 0.1s, color 0.1s;
      white-space: nowrap;
    }
    .gi-row:hover    { background: var(--bg-hover); color: var(--text-hi); }

    .gi-caret {
      display: flex; align-items: center; justify-content: center;
      width: 1.2em; flex-shrink: 0; color: var(--text-dim);
    }
    .gi-caret.open svg { transform: rotate(90deg); }

    .gi-from {
      font-weight: 600;
      color: var(--text-hi);
      overflow: hidden;
      text-overflow: ellipsis;
      max-width: 20ch;
      flex-shrink: 0;
    }
    .gi-sep {
      color: var(--text-dim);
      flex-shrink: 0;
    }
    .gi-subject {
      flex: 1;
      overflow: hidden;
      text-overflow: ellipsis;
      color: var(--text);
    }
    .gi-trash {
      flex-shrink: 0;
      background: none;
      border: none;
      color: var(--red, #e53935);
      cursor: pointer;
      padding: 0.1rem 0.3rem;
      border-radius: 0.2rem;
      font-size: 0.85rem;
      line-height: 1;
      opacity: 0.5;
      transition: opacity 0.1s;
    }
    .gi-trash:hover { opacity: 1; background: var(--bg-hover); }

    .gi-body {
      display: none;
      padding: 0.4rem 0.5rem 0.4rem 2.5rem;
      font-family: ${FONT_MONO};
      font-size: var(--fs-sm, 0.70rem);
      line-height: 1.6;
      color: var(--text);
    }
    .gi-body.open { display: block; }

    .gi-header-line {
      color: var(--text-dim);
      white-space: nowrap;
      overflow: hidden;
      text-overflow: ellipsis;
    }
    .gi-header-line strong { color: var(--text-hi); }

    .gi-body-text {
      margin-top: 0.4rem;
      padding-top: 0.4rem;
      border-top: 1px solid var(--border, rgba(255,255,255,0.1));
      white-space: pre-wrap;
      word-break: break-word;
    }

    .gi-error {
      padding: 0.2rem 0.5rem;
      color: var(--red, #e53935);
      font-size: var(--fs-sm, 0.70rem);
    }

    .gi-empty {
      padding: 0.2rem 0.5rem;
      color: var(--text-dim);
      font-size: var(--fs-sm, 0.70rem);
    }
  `);
}

export default function createWidget({ name, evalContext = {}, args = [] }) {
  ensureStyles();

  const el = document.createElement('div');
  el.className = 'gi-tree';
  el.style.cssText = 'overflow:auto;flex:1;min-height:0;height:100%;';

  const fs     = evalContext.fs;
  const shell  = evalContext.shell;
  const serverUrl = shell?.serverUrl || window.location.origin;
  const volume = shell?.volume || 'primary';

  let messages = new Map();
  let username = null;
  let inboxDir = null;
  let refreshTimer = null;

  const decoration = { title: 'inbox', leftButtons: [] };

  async function getUsername() {
    if (!fs) return null;
    const identities = await fs.whoami(serverUrl);
    return identities?.[0]?.username || null;
  }

  async function load() {
    el.innerHTML = '';

    if (!username) {
      username = await getUsername();
      if (!username) {
        el.appendChild(msgEl('gi-error', 'not logged in'));
        return;
      }
      inboxDir = `home/${username}/local/inbox`;
      decoration.title = `inbox — ${username}`;
    }

    const vol = fs.volume(serverUrl, 'primary');

    let rootFile;
    try {
      rootFile = await vol.lookup(inboxDir);
    } catch (e) {
      el.appendChild(msgEl('gi-error', `cannot open inbox: ${e.message}`));
      return;
    }

    let childFiles;
    try {
      childFiles = await rootFile.children();
    } catch (e) {
      el.appendChild(msgEl('gi-error', e.message));
      return;
    }

    const sorted = [...childFiles.entries()]
      .filter(([, f]) => !f.isDir())
      .sort(([a], [b]) => a.localeCompare(b));

    const newMessages = new Map();
    for (const [name, file] of sorted) {
      const existing = messages.get(name);
      if (existing && existing.file.cid() === file.cid()) {
        newMessages.set(name, existing);
        el.appendChild(existing.el.row);
        if (existing.el.bodyEl) el.appendChild(existing.el.bodyEl);
      } else {
        const entry = { name, file, expanded: false, content: null, el: {} };
        buildRow(entry);
        newMessages.set(name, entry);
        el.appendChild(entry.el.row);
        if (entry.el.bodyEl) el.appendChild(entry.el.bodyEl);
      }
    }
    messages = newMessages;

    if (sorted.length === 0) {
      el.appendChild(msgEl('gi-empty', '(empty)'));
    }
  }

  function buildRow(entry) {
    const row = document.createElement('div');
    row.className = 'gi-row';

    const caretEl = document.createElement('span');
    caretEl.className = 'gi-caret';
    caretEl.innerHTML = SVG_CARET;
    row.appendChild(caretEl);

    const fromEl = document.createElement('span');
    fromEl.className = 'gi-from';
    fromEl.textContent = '...';
    row.appendChild(fromEl);

    const sepEl = document.createElement('span');
    sepEl.className = 'gi-sep';
    sepEl.textContent = '—';
    row.appendChild(sepEl);

    const subjectEl = document.createElement('span');
    subjectEl.className = 'gi-subject';
    subjectEl.textContent = '...';
    row.appendChild(subjectEl);

    const trashBtn = document.createElement('button');
    trashBtn.className = 'gi-trash';
    trashBtn.textContent = '✕';
    trashBtn.title = 'Delete message';
    row.appendChild(trashBtn);

    const bodyEl = document.createElement('div');
    bodyEl.className = 'gi-body';

    entry.el = { row, caretEl, fromEl, subjectEl, trashBtn, bodyEl };

    row.addEventListener('click', (e) => {
      if (e.target === trashBtn) return;
      toggle(entry);
    });

    trashBtn.addEventListener('click', (e) => {
      e.stopPropagation();
      trash(entry);
    });

    loadMessagePreview(entry);
  }

  async function loadMessagePreview(entry) {
    try {
      const data = await entry.file.json();
      if (!data || typeof data !== 'object' || Array.isArray(data)) {
        entry.el.fromEl.textContent = '(invalid)';
        entry.el.subjectEl.textContent = '(not a JSON message)';
        return;
      }
      entry.el.fromEl.textContent = data.from || '(anonymous)';
      entry.el.subjectEl.textContent = data.subject || '(no subject)';
      entry.content = data;
    } catch (e) {
      entry.el.fromEl.textContent = '(error)';
      entry.el.subjectEl.textContent = e.message;
    }
  }

  function toggle(entry) {
    entry.expanded = !entry.expanded;

    if (entry.expanded) {
      entry.el.caretEl.classList.add('open');
      entry.el.bodyEl.classList.add('open');
      renderBody(entry);
    } else {
      entry.el.caretEl.classList.remove('open');
      entry.el.bodyEl.classList.remove('open');
    }
  }

  function renderBody(entry) {
    const data = entry.content;
    if (!data) {
      entry.el.bodyEl.innerHTML = '<div class="gi-error">(could not load)</div>';
      return;
    }

    const parts = [];
    parts.push(`<div class="gi-header-line"><strong>From:</strong> ${esc(data.from || '(anonymous)')}</div>`);
    if (data.to) parts.push(`<div class="gi-header-line"><strong>To:</strong> ${esc(data.to)}</div>`);
    if (data.subject) parts.push(`<div class="gi-header-line"><strong>Subject:</strong> ${esc(data.subject)}</div>`);

    const bodyHtml = data.bodyHtml;
    const bodyMarkdown = data.bodyMarkdown;
    const bodyText = data.body || data.bodyText || '';

    if (bodyHtml) {
      parts.push(`<div class="gi-body-text">${bodyHtml}</div>`);
    } else {
      parts.push(`<div class="gi-body-text">${esc(bodyText || bodyMarkdown || '')}</div>`);
    }

    entry.el.bodyEl.innerHTML = parts.join('');
  }

  async function trash(entry) {
    try {
      const vol = fs.volume(serverUrl, 'primary');
      await vol.multiLink([{
        path: `${inboxDir}/${entry.name}`,
        addr: '',
        assert: ASSERT_IS_BLOB,
      }]);
    } catch (e) {
      toast(`Failed to delete: ${e.message}`);
      return;
    }
    entry.el.row.remove();
    if (entry.el.bodyEl) entry.el.bodyEl.remove();
    messages.delete(entry.name);
  }

  function msgEl(cls, text) {
    const d = document.createElement('div');
    d.className = cls;
    d.textContent = text;
    return d;
  }

  function esc(s) {
    if (typeof s !== 'string') return '';
    const map = { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' };
    return s.replace(/[&<>"']/g, c => map[c]);
  }

  async function refreshTick() {
    if (!inboxDir || !username) return;
    const vol = fs.volume(serverUrl, 'primary');
    try {
      const rootFile = await vol.lookup(inboxDir);
      const childFiles = await rootFile.children();

      const currentNames = new Set(messages.keys());
      const newNames = new Set();

      for (const [name, f] of childFiles) {
        if (f.isDir()) continue;
        newNames.add(name);
        const existing = messages.get(name);
        if (existing && existing.file.cid() === f.cid()) continue;

        const entry = { name, file: f, expanded: false, content: null, el: {} };
        buildRow(entry);
        messages.set(name, entry);

        let insertBefore = null;
        for (const [sibName, sibEntry] of messages) {
          if (sibName > name) { insertBefore = sibEntry.el.row; break; }
        }
        if (insertBefore) {
          el.insertBefore(entry.el.row, insertBefore);
          if (entry.el.bodyEl) el.insertBefore(entry.el.bodyEl, insertBefore);
        } else {
          el.appendChild(entry.el.row);
          if (entry.el.bodyEl) el.appendChild(entry.el.bodyEl);
        }
      }

      for (const name of currentNames) {
        if (!newNames.has(name)) {
          const entry = messages.get(name);
          entry.el.row.remove();
          if (entry.el.bodyEl) entry.el.bodyEl.remove();
          messages.delete(name);
        }
      }

      if (newNames.size === 0 && messages.size === 0) {
        if (!el.querySelector('.gi-empty')) {
          el.appendChild(msgEl('gi-empty', '(empty)'));
        }
      } else {
        const emptyEl = el.querySelector('.gi-empty');
        if (emptyEl) emptyEl.remove();
      }
    } catch (e) {
      // silently retry next tick
    }
  }

  function startAutoRefresh() {
    stopAutoRefresh();
    refreshTimer = setInterval(refreshTick, 2000);
  }

  function stopAutoRefresh() {
    if (refreshTimer) {
      clearInterval(refreshTimer);
      refreshTimer = null;
    }
  }

  load().then(startAutoRefresh);

  el.tabIndex = 0;
  el.style.outline = 'none';
  return {
    el,
    decoration,
    focus() { el.focus(); },
    destroy() { stopAutoRefresh(); },
  };
}
