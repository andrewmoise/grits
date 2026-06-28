/*
 * @cell inbox-widget  v0.2
 * @about
 *   Inbox reader for the Gimbal shell. Lists messages dropped into
 *   the user's local/inbox directory with newest first. Click a row
 *   to open the message in a resizable detail pane below.
 * @implements gimbal-shell#widget
 */

import { injectStyles } from '../style/style.js';
import { toast } from '../gimbal/dialog.js';
import { ASSERT_IS_BLOB } from '../grits/GritsClient.js';

const STYLE_ID = 'gimbal-inbox-styles';
const MIN_PANE = 60;

function ensureStyles() {
  injectStyles(STYLE_ID, `
    .gi-wrap {
      display: flex;
      flex-direction: column;
      height: 100%;
      overflow: hidden;
      font-family: var(--font-ui);
      font-size: var(--fs-md);
      line-height: 1.6;
      user-select: none;
    }

    .gi-list {
      flex: 1;
      overflow-y: auto;
      min-height: 0;
      padding: 0.25rem 0;
    }

    .gi-row {
      display: flex;
      align-items: center;
      gap: 0.375rem;
      padding: 0.25rem 0.5rem;
      cursor: pointer;
      border-radius: 0.3rem;
      color: var(--text);
      transition: background 0.1s;
    }
    .gi-row:hover  { background: var(--bg-hover); color: var(--text-hi); }
    .gi-row.selected { background: var(--bg-active); color: var(--text-hi); }

    .gi-row-sender {
      color: var(--a1);
      white-space: nowrap;
      overflow: hidden;
      text-overflow: ellipsis;
      flex-shrink: 0;
      max-width: 25%;
    }
    .gi-row.selected .gi-row-sender { color: var(--text-hi); }

    .gi-row-sep {
      flex-shrink: 0;
      color: var(--text-dim);
    }

    .gi-row-subject {
      flex: 1;
      white-space: nowrap;
      overflow: hidden;
      text-overflow: ellipsis;
      color: var(--text-dim);
    }
    .gi-row.selected .gi-row-subject { color: var(--text); }

    .gi-row-time {
      flex-shrink: 0;
      white-space: nowrap;
      color: var(--text-dim);
      font-size: var(--fs-sm);
    }

    .gi-trash {
      flex-shrink: 0;
      background: none;
      border: none;
      color: var(--red, #e53935);
      cursor: pointer;
      padding: 0.1rem 0.3rem;
      border-radius: 0.2rem;
      font-size: 0.8rem;
      line-height: 1;
      opacity: 0.5;
      transition: opacity 0.1s;
    }
    .gi-trash:hover { opacity: 1; background: var(--bg-hover); }

    .gi-splitter {
      height: 4px;
      background: var(--border, rgba(255,255,255,0.12));
      cursor: row-resize;
      flex-shrink: 0;
      display: none;
      position: relative;
    }
    .gi-splitter::after {
      content: '';
      position: absolute;
      left: 0; right: 0; top: -3px; bottom: -3px;
    }
    .gi-splitter.dragging {
      background: var(--a2, #4a9eff);
    }

    .gi-detail {
      overflow-y: auto;
      min-height: 0;
      display: none;
      flex-direction: column;
      padding: 0.5rem 0.75rem;
      border-top: none;
    }

    .gi-detail-header {
      display: grid;
      grid-template-columns: auto 1fr;
      gap: 0.15rem 0.75rem;
      align-items: baseline;
      margin-bottom: 0.5rem;
    }
    .gi-detail-label {
      color: var(--a1);
      white-space: nowrap;
    }
    .gi-detail-value {
      color: var(--text-hi);
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
    }

    .gi-detail-sep {
      border: none;
      border-top: 1px solid var(--border, rgba(255,255,255,0.12));
      margin: 0.25rem 0 0.5rem;
    }

    .gi-detail-body {
      color: var(--text);
      white-space: pre-wrap;
      word-break: break-word;
      line-height: 1.65;
    }

    .gi-error {
      padding: 0.3rem 0.5rem;
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

function formatTimestamp(ts) {
  const d = new Date(ts);
  if (isNaN(d.getTime())) return '';
  const now = new Date();
  const diffMs = now - d;
  const diffHours = diffMs / (1000 * 60 * 60);

  const sameDay = d.getFullYear() === now.getFullYear() &&
    d.getMonth() === now.getMonth() &&
    d.getDate() === now.getDate();

  if (sameDay || diffHours < 5) {
    const hh = d.getHours().toString().padStart(2, '0');
    const mm = d.getMinutes().toString().padStart(2, '0');
    return `${hh}:${mm}`;
  }

  const diffDays = diffMs / (1000 * 60 * 60 * 24);
  if (diffDays < 5) {
    return d.toLocaleDateString('en-US', { weekday: 'short' });
  }

  return d.toLocaleDateString('en-US', { month: 'short', day: 'numeric' });
}

export default function createWidget({ name, gimbal, args = [] }) {
  ensureStyles();

  const el = document.createElement('div');
  el.className = 'gi-wrap';

  const serverUrl = gimbal?._serverUrl || window.location.origin;

  let messages = new Map();
  let username = null;
  let inboxDir = null;
  let refreshTimer = null;
  let selectedKey = null;
  let detailVisible = false;

  const decoration = { title: '', leftButtons: [] };

  // ── Build DOM structure ──────────────────────────────

  const listEl = document.createElement('div');
  listEl.className = 'gi-list';

  const splitter = document.createElement('div');
  splitter.className = 'gi-splitter';

  const detailEl = document.createElement('div');
  detailEl.className = 'gi-detail';

  el.appendChild(listEl);
  el.appendChild(splitter);
  el.appendChild(detailEl);

  // ── Splitter drag logic ─────────────────────────────

  function bindSplitter() {
    let dragging = false;
    let startY = 0;
    let startH = 0;

    splitter.addEventListener('mousedown', (e) => {
      e.preventDefault();
      dragging = true;
      startY = e.clientY;
      startH = detailEl.getBoundingClientRect().height;
      splitter.classList.add('dragging');

      function onMove(e) {
        if (!dragging) return;
        const delta = e.clientY - startY;
        let newH = startH - delta;
        const avail = el.getBoundingClientRect().height - splitter.offsetHeight;
        newH = Math.max(MIN_PANE, Math.min(newH, avail - MIN_PANE));
        detailEl.style.height = newH + 'px';
        detailEl.style.flex = 'none';
      }

      function onUp() {
        dragging = false;
        splitter.classList.remove('dragging');
        document.removeEventListener('mousemove', onMove);
        document.removeEventListener('mouseup', onUp);
      }

      document.addEventListener('mousemove', onMove);
      document.addEventListener('mouseup', onUp);
    });
  }
  bindSplitter();

  // ── Show detail pane ────────────────────────────────

  function showDetail(entry) {
    if (!entry.content) return;

    if (!detailVisible) {
      detailVisible = true;
      splitter.style.display = 'block';
      detailEl.style.display = 'flex';
      const avail = el.getBoundingClientRect().height - splitter.offsetHeight;
      detailEl.style.height = Math.min(240, avail * 0.4) + 'px';
      detailEl.style.flex = 'none';
    }

    renderDetail(entry);
  }

  function hideDetail() {
    detailVisible = false;
    splitter.style.display = 'none';
    detailEl.style.display = 'none';
    detailEl.innerHTML = '';
    detailEl.style.height = '';
    detailEl.style.flex = '';
  }

  // ── Data loading ────────────────────────────────────

  async function getUsername() {
    const identities = await gimbal.grits.whoami(serverUrl);
    return identities?.[0]?.username || null;
  }

  async function load() {
    listEl.innerHTML = '';

    if (!username) {
      username = await getUsername();
      if (!username) {
        listEl.appendChild(msgEl('gi-error', 'not logged in'));
        return;
      }
      inboxDir = `home/${username}/local/inbox`;
    }

    const vol = gimbal.grits.volume(serverUrl, 'primary');

    let rootFile;
    try {
      rootFile = await vol.lookup(inboxDir);
    } catch (e) {
      listEl.appendChild(msgEl('gi-error', `cannot open inbox: ${e.message}`));
      return;
    }

    let childFiles;
    try {
      childFiles = await rootFile.children();
    } catch (e) {
      listEl.appendChild(msgEl('gi-error', e.message));
      return;
    }

    const sorted = [...childFiles.entries()]
      .filter(([, f]) => !f.isDir())
      .sort(([a], [b]) => b.localeCompare(a));

    const newMessages = new Map();
    for (const [name, file] of sorted) {
      const existing = messages.get(name);
      if (existing && existing.file.cid() === file.cid()) {
        newMessages.set(name, existing);
        listEl.appendChild(existing.el.row);
      } else {
        const entry = { name, file, expanded: false, content: null, el: {} };
        buildRow(entry);
        newMessages.set(name, entry);
        listEl.appendChild(entry.el.row);
      }
    }
    messages = newMessages;

    if (sorted.length === 0) {
      listEl.appendChild(msgEl('gi-empty', '(empty)'));
    }

    if (selectedKey && !messages.has(selectedKey)) {
      selectedKey = null;
      hideDetail();
    }
  }

  // ── Row building ────────────────────────────────────

  function buildRow(entry) {
    const row = document.createElement('div');
    row.className = 'gi-row';

    const senderEl = document.createElement('span');
    senderEl.className = 'gi-row-sender';
    senderEl.textContent = '...';
    row.appendChild(senderEl);

    const sepEl = document.createElement('span');
    sepEl.className = 'gi-row-sep';
    sepEl.textContent = ' - ';
    row.appendChild(sepEl);

    const subjectEl = document.createElement('span');
    subjectEl.className = 'gi-row-subject';
    subjectEl.textContent = '';
    row.appendChild(subjectEl);

    const timeEl = document.createElement('span');
    timeEl.className = 'gi-row-time';
    timeEl.textContent = '';
    row.appendChild(timeEl);

    const trashBtn = document.createElement('button');
    trashBtn.className = 'gi-trash';
    trashBtn.textContent = '✕';
    trashBtn.title = 'Delete message';
    row.appendChild(trashBtn);

    entry.el = { row, senderEl, sepEl, subjectEl, timeEl, trashBtn };

    row.addEventListener('click', (e) => {
      if (e.target === trashBtn || e.target.closest('.gi-trash')) return;
      selectEntry(entry);
    });

    trashBtn.addEventListener('click', (e) => {
      e.stopPropagation();
      trash(entry);
    });

    loadMessagePreview(entry);
  }

  // ── Preview loading ─────────────────────────────────

  async function loadMessagePreview(entry) {
    try {
      const data = await entry.file.json();
      if (!data || typeof data !== 'object' || Array.isArray(data)) {
        entry.el.senderEl.textContent = '(invalid)';
        entry.el.subjectEl.textContent = '(not a JSON message)';
        return;
      }
      entry.content = data;
      entry.el.senderEl.textContent = data.from || '(anonymous)';
      const preview = data.subject
        ? data.subject
        : (data.body ? data.body.slice(0, 60).replace(/\n.*/, '').trim() : '');
      entry.el.subjectEl.textContent = preview || '';
      entry.el.timeEl.textContent = formatTimestamp(data.timestamp);
    } catch (e) {
      entry.el.senderEl.textContent = '(error)';
      entry.el.subjectEl.textContent = '';
    }
  }

  // ── Selection ───────────────────────────────────────

  function selectEntry(entry) {
    if (selectedKey) {
      const prev = messages.get(selectedKey);
      if (prev) prev.el.row.classList.remove('selected');
    }
    selectedKey = entry.name;
    entry.el.row.classList.add('selected');

    if (entry.content) {
      showDetail(entry);
    }
  }

  // ── Detail rendering ────────────────────────────────

  function renderDetail(entry) {
    const data = entry.content;
    if (!data) {
      detailEl.innerHTML = '<div class="gi-error">(could not load)</div>';
      return;
    }

    const parts = ['<div class="gi-detail-header">'];

    parts.push(`<div class="gi-detail-label">From:</div>`);
    parts.push(`<div class="gi-detail-value">${esc(data.from || '(anonymous)')}</div>`);

    if (data.to) {
      parts.push(`<div class="gi-detail-label">To:</div>`);
      parts.push(`<div class="gi-detail-value">${esc(data.to)}</div>`);
    }
    if (data.subject) {
      parts.push(`<div class="gi-detail-label">Subject:</div>`);
      parts.push(`<div class="gi-detail-value">${esc(data.subject)}</div>`);
    }

    const ts = data.timestamp;
    if (ts) {
      const d = new Date(ts);
      if (!isNaN(d.getTime())) {
        parts.push(`<div class="gi-detail-label">Date:</div>`);
        parts.push(`<div class="gi-detail-value">${esc(d.toLocaleString())}</div>`);
      }
    }

    parts.push('</div>');

    parts.push('<hr class="gi-detail-sep">');

    const bodyHtml = data.bodyHtml;
    const bodyMarkdown = data.bodyMarkdown;
    const bodyText = data.body || data.bodyText || '';

    if (bodyHtml) {
      parts.push(`<div class="gi-detail-body">${bodyHtml}</div>`);
    } else {
      parts.push(`<div class="gi-detail-body">${esc(bodyText || bodyMarkdown || '')}</div>`);
    }

    detailEl.innerHTML = parts.join('');
  }

  // ── Trash / delete ──────────────────────────────────

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
    entry.el.row.remove();
    messages.delete(entry.name);
    if (selectedKey === entry.name) {
      selectedKey = null;
      hideDetail();
    }
  }

  // ── Helpers ─────────────────────────────────────────

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

  // ── Auto-refresh ────────────────────────────────────

  async function refreshTick() {
    if (!inboxDir || !username) return;
    const vol = gimbal.grits.volume(serverUrl, 'primary');
    try {
      const rootFile = await vol.lookup(inboxDir);
      const childFiles = await rootFile.children();

      const currentNames = new Set(messages.keys());
      const newNames = new Set();

      const additions = [];

      for (const [name, f] of childFiles) {
        if (f.isDir()) continue;
        newNames.add(name);
        const existing = messages.get(name);
        if (existing && existing.file.cid() === f.cid()) {
          existing.el.timeEl.textContent = formatTimestamp(existing.content?.timestamp);
          continue;
        }

        const entry = { name, file: f, expanded: false, content: null, el: {} };
        buildRow(entry);
        messages.set(name, entry);
        additions.push({ name, row: entry.el.row });
      }

      for (const { name, row } of additions) {
        let insertBefore = null;
        for (const [sibName, sibEntry] of messages) {
          if (sibName < name) { insertBefore = sibEntry.el.row; break; }
        }
        if (insertBefore) {
          listEl.insertBefore(row, insertBefore);
        } else {
          listEl.appendChild(row);
        }
      }

      for (const name of currentNames) {
        if (!newNames.has(name)) {
          const entry = messages.get(name);
          entry.el.row.remove();
          messages.delete(name);
          if (selectedKey === name) {
            selectedKey = null;
            hideDetail();
          }
        }
      }

      if (newNames.size === 0 && messages.size === 0) {
        if (!listEl.querySelector('.gi-empty')) {
          listEl.appendChild(msgEl('gi-empty', '(empty)'));
        }
      } else {
        const emptyEl = listEl.querySelector('.gi-empty');
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
    focus() { listEl.focus(); },
    destroy() { stopAutoRefresh(); },
  };
}
