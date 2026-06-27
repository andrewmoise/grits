/*
 * @cell inbox-widget  v0.1
 * @about
 *   Inbox reader for the Gimbal shell. Lists messages dropped into
 *   the user's local/inbox directory. Each row shows From and Subject;
 *   expand to read the full message. Trash button deletes messages.
 * @implements gimbal-shell#widget
 */

import { injectStyles } from '../style/style.js';
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
      font-family: var(--font-ui);
      font-size: var(--fs-md);
      line-height: 1.75;
      user-select: none;
      overflow-y: auto;
    }

    .gi-message {
      display: flex;
      align-items: flex-start;
      gap: 0.25rem;
      padding: 0.2rem 0.5rem;
      cursor: pointer;
      border-radius: 0.3rem;
      color: var(--text);
      transition: background 0.1s, color 0.1s;
    }
    .gi-message:hover  { background: var(--bg-hover); color: var(--text-hi); }
    .gi-message.expanded .gi-preview-sep,
    .gi-message.expanded .gi-preview { display: none; }

    .gi-caret {
      display: flex; align-items: center; justify-content: center;
      width: 1.2em; flex-shrink: 0; color: var(--text-dim);
      padding-top: 0.1em;
    }
    .gi-caret.open svg { transform: rotate(90deg); }

    .gi-content {
      flex: 1;
      min-width: 0;
    }

    .gi-preview-line {
      white-space: nowrap;
      overflow: hidden;
      text-overflow: ellipsis;
    }
    .gi-preview-line .gi-from-label {
      color: var(--a1);
    }
    .gi-preview-line .gi-from {
      color: var(--text);
    }
    .gi-preview-line .gi-preview-sep {
      color: var(--a1);
    }
    .gi-preview-line .gi-preview {
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
      margin-top: 0.1em;
    }
    .gi-trash:hover { opacity: 1; background: var(--bg-hover); }

    .gi-detail {
      max-height: 0;
      overflow: hidden;
      transition: max-height 0.25s ease;
    }
    .gi-detail.open {
      max-height: 2000px;
    }

    .gi-header-line {
      margin-top: 0.15rem;
      white-space: nowrap;
      overflow: hidden;
      text-overflow: ellipsis;
    }
    .gi-header-line strong {
      color: var(--a1);
    }
    .gi-hvalue {
      color: var(--text-hi);
    }

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
      font-size: var(--fs-md);
    }

    .gi-empty {
      padding: 0.5rem 0.5rem;
      color: var(--text-dim);
      font-size: var(--fs-md);
      text-align: center;
    }
  `);
}

export default function createWidget({ name, gimbal, args = [] }) {
  ensureStyles();

  const el = document.createElement('div');
  el.className = 'gi-tree';
  el.style.cssText = 'overflow:auto;flex:1;min-height:0;height:100%;';

  const serverUrl = gimbal?._serverUrl || window.location.origin;
  const volume = 'primary';

  let messages = new Map();
  let username = null;
  let inboxDir = null;
  let refreshTimer = null;

  const decoration = { title: '', leftButtons: [] };

  async function getUsername() {
    const identities = await gimbal.grits.whoami(serverUrl);
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
    }

    const vol = gimbal.grits.volume(serverUrl, 'primary');

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
        el.appendChild(existing.el.message);
      } else {
        const entry = { name, file, expanded: false, content: null, el: {} };
        buildRow(entry);
        newMessages.set(name, entry);
        el.appendChild(entry.el.message);
      }
    }
    messages = newMessages;

    if (sorted.length === 0) {
      el.appendChild(msgEl('gi-empty', '(empty)'));
    }
  }

  function buildRow(entry) {
    const message = document.createElement('div');
    message.className = 'gi-message';

    const caretEl = document.createElement('span');
    caretEl.className = 'gi-caret';
    caretEl.innerHTML = SVG_CARET;
    message.appendChild(caretEl);

    const content = document.createElement('div');
    content.className = 'gi-content';

    const previewLine = document.createElement('div');
    previewLine.className = 'gi-preview-line';

    const fromLabelEl = document.createElement('span');
    fromLabelEl.className = 'gi-from-label';
    fromLabelEl.textContent = 'From:';
    previewLine.appendChild(fromLabelEl);

    const fromEl = document.createElement('span');
    fromEl.className = 'gi-from';
    fromEl.textContent = '...';
    previewLine.appendChild(fromEl);

    const previewSepEl = document.createElement('span');
    previewSepEl.className = 'gi-preview-sep';
    previewSepEl.textContent = '';
    previewLine.appendChild(previewSepEl);

    const previewEl = document.createElement('span');
    previewEl.className = 'gi-preview';
    previewEl.textContent = '';
    previewLine.appendChild(previewEl);

    content.appendChild(previewLine);

    const detail = document.createElement('div');
    detail.className = 'gi-detail';
    content.appendChild(detail);

    message.appendChild(content);

    const trashBtn = document.createElement('button');
    trashBtn.className = 'gi-trash';
    trashBtn.textContent = '✕';
    trashBtn.title = 'Delete message';
    message.appendChild(trashBtn);

    entry.el = { message, content, caretEl, fromLabelEl, fromEl, previewSepEl, previewEl, trashBtn, detail };

    message.addEventListener('click', (e) => {
      if (e.target === trashBtn || e.target.closest('.gi-trash')) return;
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
        entry.el.previewSepEl.textContent = '';
        entry.el.previewEl.textContent = ' (not a JSON message)';
        return;
      }
      entry.el.fromEl.textContent = data.from || '(anonymous)';
      const preview = data.subject
        ? data.subject
        : (data.body ? data.body.slice(0, 60).replace(/\n.*/, '').trim() : '');
      if (preview) {
        entry.el.previewSepEl.textContent = ' / ';
        entry.el.previewEl.textContent = preview;
      } else {
        entry.el.previewSepEl.textContent = '';
        entry.el.previewEl.textContent = '';
      }
      entry.content = data;
    } catch (e) {
      entry.el.fromEl.textContent = '(error)';
      entry.el.previewSepEl.textContent = '';
      entry.el.previewEl.textContent = '';
    }
  }

  function toggle(entry) {
    entry.expanded = !entry.expanded;

    if (entry.expanded) {
      entry.el.caretEl.classList.add('open');
      entry.el.message.classList.add('expanded');
      entry.el.detail.classList.add('open');
      renderDetail(entry);
    } else {
      entry.el.caretEl.classList.remove('open');
      entry.el.message.classList.remove('expanded');
      entry.el.detail.classList.remove('open');
    }
  }

  function renderDetail(entry) {
    const data = entry.content;
    if (!data) {
      entry.el.detail.innerHTML = '<div class="gi-error">(could not load)</div>';
      return;
    }

    const parts = [];
    if (data.to) parts.push(`<div class="gi-header-line"><strong>To:</strong> <span class="gi-hvalue">${esc(data.to)}</span></div>`);
    if (data.subject) parts.push(`<div class="gi-header-line"><strong>Subject:</strong> <span class="gi-hvalue">${esc(data.subject)}</span></div>`);

    const bodyHtml = data.bodyHtml;
    const bodyMarkdown = data.bodyMarkdown;
    const bodyText = data.body || data.bodyText || '';

    if (bodyHtml) {
      parts.push(`<div class="gi-body-text">${bodyHtml}</div>`);
    } else {
      parts.push(`<div class="gi-body-text">${esc(bodyText || bodyMarkdown || '')}</div>`);
    }

    entry.el.detail.innerHTML = parts.join('');
  }

  async function trash(entry) {
    try {
      const vol = gimbal.grits.volume(serverUrl, 'primary');
      await vol.multiLink([{
        path: `${inboxDir}/${entry.name}`,
        addr: '',
        assert: ASSERT_IS_BLOB,
      }]);
    } catch (e) {
      toast(`Failed to delete: ${e.message}`);
      return;
    }
    entry.el.message.remove();
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
    const vol = gimbal.grits.volume(serverUrl, 'primary');
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
          if (sibName > name) { insertBefore = sibEntry.el.message; break; }
        }
        if (insertBefore) {
          el.insertBefore(entry.el.message, insertBefore);
        } else {
          el.appendChild(entry.el.message);
        }
      }

      for (const name of currentNames) {
        if (!newNames.has(name)) {
          const entry = messages.get(name);
          entry.el.message.remove();
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
