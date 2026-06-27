/*
 * @cell tracker-widget
 * @version 0.3
 * @about
 *   Project tracker backed by Grits filesystem via GritsClient directly.
 *   Opens or creates a project directory. Auto-saves on every change.
 *   Data lives in project.json in the given directory.
 *   Pass { path } in opts to pre-populate the path field.
 * @implements gimbal-shell#widget
 */

import { ASSERT_PREV_MATCHES, AssertionError } from '../grits/GritsClient.js';

// ── Constants ─────────────────────────────────────────────────────────────────

const STATE_COLOR = {
  'ready':           '#888780',
  'in-progress':     '#d4954a',
  'post-production': '#378ADD',
  'can-ship':        '#3fb97a',
};
const STATE_LABEL = {
  'ready':           'ready',
  'in-progress':     'in production',
  'post-production': 'in post',
  'can-ship':        'can ship',
};
const STATE_NEXT = {
  'ready':           'in-progress',
  'in-progress':     'post-production',
  'post-production': 'can-ship',
  'can-ship':        'ready',
};
const STATE_ORDER = {
  'ready': 0, 'in-progress': 1, 'post-production': 2, 'can-ship': 3,
};
const STATE_PILL = {
  'ready':           'p-ready',
  'in-progress':     'p-prog',
  'post-production': 'p-post',
  'can-ship':        'p-ship',
};

// ── ID management ─────────────────────────────────────────────────────────────

let _nextId = 1;
function newId() { return String(_nextId++); }

function fixIds(item) {
  if (!item.id)           item.id       = newId();
  if (!item.children)     item.children = [];
  if (!item.state)        item.state    = 'ready';
  if (item.budget == null) item.budget  = 8;
  if (item.used   == null) item.used    = 0;
  if (!item.assignee)     item.assignee = '';
  if (!item.desc)         item.desc     = '';
  const parsed = parseInt(item.id);
  if (!isNaN(parsed)) _nextId = Math.max(_nextId, parsed + 1);
  item.children.forEach(fixIds);
}

// ── Tree helpers ──────────────────────────────────────────────────────────────

function makeItem(name) {
  return {
    id: newId(), name, state: 'ready',
    budget: 8, used: 0, assignee: '', desc: '', children: [],
  };
}

function allLeaves(item) {
  if (!item.children.length) return [item];
  return item.children.flatMap(allLeaves);
}

function rollup(item) {
  if (!item.children.length) return { budget: item.budget || 0, used: item.used || 0 };
  const kids = item.children.map(rollup);
  return {
    budget: kids.reduce((s, k) => s + k.budget, 0),
    used:   kids.reduce((s, k) => s + k.used,   0),
  };
}

function rolledState(item) {
  if (!item.children.length) return item.state;
  const orders = allLeaves(item).map(l => STATE_ORDER[l.state] ?? 0);
  const min = Math.min(...orders);
  const max = Math.max(...orders);
  if (min === STATE_ORDER['can-ship'])       return 'can-ship';
  if (min >= STATE_ORDER['post-production']) return 'post-production';
  if (max >= STATE_ORDER['in-progress'])     return 'in-progress';
  return 'ready';
}

function findParentList(list, id) {
  for (const n of list) {
    if (n.children.some(c => c.id === id)) return n.children;
    const f = findParentList(n.children, id);
    if (f) return f;
  }
  return null;
}

function removeFromTree(list, id) {
  const idx = list.findIndex(n => n.id === id);
  if (idx !== -1) { list.splice(idx, 1); return true; }
  return list.some(n => removeFromTree(n.children, id));
}

function findInTree(list, id) {
  for (const n of list) {
    if (n.id === id) return n;
    const f = findInTree(n.children, id);
    if (f) return f;
  }
  return null;
}

// ── Widget ────────────────────────────────────────────────────────────────────

export default function createWidget({ name, gimbal, opts = {} }) {
  const grits = gimbal?.grits;

  let vol = null;
  if (!vol && grits) {
    try { vol = grits.volume(window.location.origin, 'sys'); } catch (_) {}
  }

  // ── Styles ───────────────────────────────────────────────────────────────────
  const STYLE_ID = 'gimbal-tracker-styles';
  if (!document.getElementById(STYLE_ID)) {
    const s = document.createElement('style');
    s.id = STYLE_ID;
    s.textContent = `
      .gtr { display:flex; flex-direction:column; width:100%; height:100%;
        font-family:'Rubik','Inter',sans-serif; }

      .gtr-pathbar { display:flex; align-items:center; gap:0.4rem;
        padding:0.4rem 0.75rem; border-bottom:1px solid var(--border);
        flex-shrink:0; }
      .gtr-pathbar input { flex:1; background:transparent; border:none; outline:none;
        color:var(--text-hi); font-family:'JetBrains Mono',monospace;
        font-size:0.75rem; caret-color:var(--a1); }
      .gtr-pathbar input::placeholder { color:var(--text-dim); }
      .gtr-pb-btn { padding:0.2rem 0.55rem; border-radius:0.3rem;
        border:1px solid var(--border); background:transparent;
        color:var(--text-dim); cursor:pointer; font-size:0.75rem;
        font-family:inherit; transition:background 0.1s,color 0.1s; flex-shrink:0; }
      .gtr-pb-btn:hover { background:var(--bg-hover); color:var(--text-hi); }

      .gtr-hdr { padding:0.75rem 0.75rem 0.6rem; border-bottom:1px solid var(--border);
        flex-shrink:0; display:flex; flex-direction:column; gap:0.45rem; }
      .gtr-hdr-top { display:flex; align-items:center; gap:0.6rem; }
      .gtr-proj-name { font-size:0.9375rem; font-weight:500; color:var(--text-hi); }
      .gtr-proj-path { font-size:0.6875rem; color:var(--text-dim);
        font-family:'JetBrains Mono',monospace; flex:1;
        white-space:nowrap; overflow:hidden; text-overflow:ellipsis; }
      .gtr-add-top { width:1.5rem; height:1.5rem; border-radius:50%;
        border:1px solid var(--border-hi); background:transparent;
        color:var(--text-dim); cursor:pointer; display:flex;
        align-items:center; justify-content:center; font-size:0.9rem;
        line-height:1; transition:background 0.1s,color 0.1s; flex-shrink:0; }
      .gtr-add-top:hover { background:var(--bg-hover); color:var(--text-hi); }

      .gtr-summary { display:flex; gap:0.375rem; flex-wrap:wrap; }
      .gtr-pill { display:flex; align-items:center; gap:0.3rem;
        padding:0.2rem 0.5rem; border-radius:999px;
        font-size:0.6875rem; border:0.5px solid; }
      .gtr-pill-dot { width:0.4rem; height:0.4rem; border-radius:50%; flex-shrink:0; }
      .p-ready { border-color:#b4b2a9; color:#5f5e5a; background:#f1efe8; }
      .p-prog  { border-color:#fac775; color:#854f0b; background:#faeeda; }
      .p-post  { border-color:#85b7eb; color:#185fa5; background:#e6f1fb; }
      .p-ship  { border-color:#9fe1cb; color:#0f6e56; background:#e1f5ee; }
      @media (prefers-color-scheme:dark) {
        .p-ready { border-color:#5f5e5a; color:#d3d1c7; background:#2c2c2a; }
        .p-prog  { border-color:#854f0b; color:#fac775; background:#412402; }
        .p-post  { border-color:#185fa5; color:#85b7eb; background:#042c53; }
        .p-ship  { border-color:#0f6e56; color:#5dcaa5; background:#04342c; }
      }

      .gtr-tree { flex:1; overflow-y:auto; padding:0.375rem 0 1rem; }

      .gtr-row { display:flex; align-items:center;
        padding:0.22rem 0.6rem 0.22rem 0; cursor:pointer;
        transition:background 0.08s; }
      .gtr-row:hover   { background:var(--bg-hover); }
      .gtr-row.gtr-sel { background:var(--bg-active); }

      .gtr-left { display:flex; align-items:center; flex:1; min-width:0; }
      .gtr-tog { width:1.1rem; height:1.5rem; display:flex; align-items:center;
        justify-content:center; color:var(--text-dim); font-size:0.5rem; flex-shrink:0; }
      .gtr-tog.leaf { visibility:hidden; }
      .gtr-dot { width:0.45rem; height:0.45rem; border-radius:50%; flex-shrink:0;
        margin-right:0.45rem; cursor:pointer; transition:transform 0.1s; }
      .gtr-dot:hover { transform:scale(1.5); }
      .gtr-name { font-size:0.8125rem; white-space:nowrap; overflow:hidden;
        text-overflow:ellipsis; min-width:0; flex:1; color:var(--text); }

      .gtr-right { display:flex; align-items:center; gap:0.4rem; flex-shrink:0; }
      .gtr-budget { font-size:0.6875rem; font-family:'JetBrains Mono',monospace;
        white-space:nowrap; text-align:right; }
      .gtr-budget .b-used  { font-weight:500; color:var(--text); }
      .gtr-budget .b-over  { font-weight:500; color:#e05555; }
      .gtr-budget .b-sep   { color:var(--text-dim); }
      .gtr-budget .b-total { color:var(--text-dim); }

      .gtr-add-btn { width:1.25rem; height:1.25rem; border-radius:50%;
        border:1px solid var(--border-hi); background:transparent;
        color:var(--text-dim); cursor:pointer; display:flex;
        align-items:center; justify-content:center; font-size:0.75rem;
        line-height:1; transition:background 0.1s,color 0.1s;
        flex-shrink:0; opacity:0; }
      .gtr-row:hover .gtr-add-btn { opacity:1; }
      .gtr-add-btn:hover { background:var(--bg-hover); color:var(--text-hi); }

      .gtr-kids { display:none; }
      .gtr-kids.open { display:block; }

      .gtr-detail { border-top:1px solid var(--border); border-bottom:1px solid var(--border);
        background:var(--bg-elevated); padding:0.5rem 0.75rem 0.5rem 2.25rem;
        display:flex; flex-direction:column; gap:0.35rem; }
      .gtr-dr { display:flex; align-items:baseline; gap:0.5rem; }
      .gtr-dl { font-size:0.625rem; color:var(--text-dim);
        font-family:'JetBrains Mono',monospace;
        width:3.75rem; flex-shrink:0; text-align:right; }
      .gtr-dinput { flex:1; background:var(--bg-float); border:1px solid var(--border);
        border-radius:0.3rem; padding:0.2rem 0.4rem; outline:none;
        color:var(--text-hi); font-family:'Rubik',sans-serif; font-size:0.75rem;
        caret-color:var(--a1); transition:border-color 0.1s; }
      .gtr-dinput:focus { border-color:var(--border-hi); }
      .gtr-dtextarea { width:100%; background:var(--bg-float); border:1px solid var(--border);
        border-radius:0.3rem; padding:0.3rem 0.4rem; outline:none;
        color:var(--text); font-family:'Rubik',sans-serif; font-size:0.75rem;
        line-height:1.5; resize:none; caret-color:var(--a1); transition:border-color 0.1s; }
      .gtr-dtextarea:focus { border-color:var(--border-hi); }
      .gtr-dselect { flex:1; background:var(--bg-float); border:1px solid var(--border);
        border-radius:0.3rem; padding:0.2rem 0.4rem; outline:none;
        color:var(--text-hi); font-family:'Rubik',sans-serif; font-size:0.75rem;
        transition:border-color 0.1s; }
      .gtr-dselect:focus { border-color:var(--border-hi); }

      .gtr-status { display:flex; align-items:center; gap:0.5rem;
        padding:0.3rem 0.75rem; border-top:1px solid var(--border);
        flex-shrink:0; font-size:0.6875rem; font-family:'JetBrains Mono',monospace; }
      .gtr-s-idle   { color:var(--text-dim); }
      .gtr-s-saving { color:var(--a1); }
      .gtr-s-saved  { color:var(--text-dim); }
      .gtr-s-error  { color:#e05555; }

      .gtr-empty { flex:1; display:flex; flex-direction:column;
        align-items:center; justify-content:center;
        gap:0.5rem; color:var(--text-dim); font-size:0.75rem; opacity:0.6; }
    `;
    document.head.appendChild(s);
  }

  // ── Widget state ──────────────────────────────────────────────────────────────
  let projectPath = opts.path || '';
  let project     = null;   // { name, items: Item[] }
  let collapsed   = new Set();
  let selId       = null;
  let detailId    = null;
  let saveTimer   = null;

  // ── DOM ───────────────────────────────────────────────────────────────────────
  const el = document.createElement('div');
  el.style.cssText = 'width:100%;height:100%;display:flex;flex-direction:column;overflow:hidden;';

  const root = document.createElement('div');
  root.className = 'gtr';

  // Path bar
  const pathBar    = document.createElement('div');
  pathBar.className = 'gtr-pathbar';
  const pathInput  = document.createElement('input');
  pathInput.placeholder = 'project directory path…';
  pathInput.value = projectPath;
  pathInput.addEventListener('keydown', e => {
    if (e.key === 'Enter') openProject(pathInput.value.trim());
  });
  const openBtn = document.createElement('button');
  openBtn.className = 'gtr-pb-btn';
  openBtn.textContent = 'open';
  openBtn.addEventListener('click', () => openProject(pathInput.value.trim()));
  pathBar.appendChild(pathInput);
  pathBar.appendChild(openBtn);

  // Header
  const hdr     = document.createElement('div');
  hdr.className = 'gtr-hdr';
  hdr.style.display = 'none';

  const hdrTop  = document.createElement('div');
  hdrTop.className = 'gtr-hdr-top';

  const projNameEl = document.createElement('span');
  projNameEl.className = 'gtr-proj-name';

  const projPathEl = document.createElement('span');
  projPathEl.className = 'gtr-proj-path';

  const addTopBtn = document.createElement('button');
  addTopBtn.className = 'gtr-add-top';
  addTopBtn.title = 'add top-level item';
  addTopBtn.textContent = '+';
  addTopBtn.addEventListener('click', () => {
    const item = makeItem('new item');
    project.items.push(item);
    scheduleSave();
    render();
    requestAnimationFrame(() => {
      const nameEl = treeEl.querySelector(`[data-id="${item.id}"] .gtr-name`);
      if (nameEl) startEdit(nameEl, item);
    });
  });

  hdrTop.appendChild(projNameEl);
  hdrTop.appendChild(projPathEl);
  hdrTop.appendChild(addTopBtn);

  const summaryEl = document.createElement('div');
  summaryEl.className = 'gtr-summary';

  hdr.appendChild(hdrTop);
  hdr.appendChild(summaryEl);

  // Tree
  const treeEl = document.createElement('div');
  treeEl.className = 'gtr-tree';

  // Status bar
  const statusBar  = document.createElement('div');
  statusBar.className = 'gtr-status';
  const statusSpan = document.createElement('span');
  statusBar.appendChild(statusSpan);

  root.appendChild(pathBar);
  root.appendChild(hdr);
  root.appendChild(treeEl);
  root.appendChild(statusBar);
  el.appendChild(root);

  // ── IO via GritsClient directly ───────────────────────────────────────────────

  // Normalise a path for use with vol.lookup / vol.link:
  // strip leading slash (server wants relative paths)
  function normPath(p) {
    return p.replace(/^\/+|\/+$/g, '');
  }

  function projectFilePath() {
    return normPath(projectPath) + '/project.json';
  }

  function setStatus(type, msg) {
    statusSpan.className = 'gtr-s-' + type;
    statusSpan.textContent = msg;
  }

  async function openProject(path) {
    if (!path) return;
    projectPath = path;
    pathInput.value = path;
    setStatus('saving', 'loading…');

    if (!vol) {
      project = { name: path.split('/').filter(Boolean).pop() || 'project', items: [] };
      setStatus('idle', 'no filesystem — changes not persisted');
      render();
      return;
    }

    try {
      const file = await vol.lookup(projectFilePath());
      const text = await file.text();
      project = JSON.parse(text);
      if (!project.items) project.items = [];
      project.items.forEach(fixIds);
      setStatus('saved', 'loaded');
    } catch (_) {
      // Doesn't exist — create directory skeleton and empty project
      const projName = path.split('/').filter(Boolean).pop() || 'project';
      project = { name: projName, items: [] };
      await ensureDir(normPath(path));
      await save();
    }
    render();
  }

  // Create a directory at path if it doesn't already exist.
  async function ensureDir(path) {
    if (!vol) return;
    try {
      const metaCID = await vol.mkdir({});
      await vol.multiLink([{
        path,
        addr:     metaCID,
        prevAddr: '',
        assert:   ASSERT_PREV_MATCHES,
      }]);
    } catch (e) {
      if (!(e instanceof AssertionError)) throw e;
      // Already exists — fine
    }
  }

  async function save() {
    if (!project) return;
    setStatus('saving', 'saving…');

    if (!vol) {
      setStatus('idle', 'no filesystem — changes not persisted');
      return;
    }

    try {
      const json       = JSON.stringify(project, null, 2);
      const bytes      = new TextEncoder().encode(json);

      // 1. Upload content blob
      const contentCID = await vol.put(bytes);

      // 2. Synthesize file metadata locally
      const metaCID    = await vol.mkfile(contentCID, bytes.byteLength);

      // 3. Link into place — unconditional overwrite
      await vol.link(metaCID, projectFilePath());

      setStatus('saved', 'saved');
    } catch (e) {
      setStatus('error', 'save failed: ' + (e.message || String(e)));
      console.error('tracker: save error', e);
    }
  }

  function scheduleSave() {
    if (saveTimer) clearTimeout(saveTimer);
    saveTimer = setTimeout(() => { saveTimer = null; save(); }, 400);
  }

  // ── Tree mutations ────────────────────────────────────────────────────────────

  function addChild(parentItem) {
    const item = makeItem('new item');
    parentItem.children.push(item);
    collapsed.delete(parentItem.id);
    scheduleSave();
    render();
    requestAnimationFrame(() => {
      const nameEl = treeEl.querySelector(`[data-id="${item.id}"] .gtr-name`);
      if (nameEl) startEdit(nameEl, item);
    });
  }

  function addSibling(item) {
    const newItem    = makeItem('new item');
    const parentList = findParentList(project.items, item.id) || project.items;
    parentList.splice(parentList.indexOf(item) + 1, 0, newItem);
    scheduleSave();
    render();
    requestAnimationFrame(() => {
      const nameEl = treeEl.querySelector(`[data-id="${newItem.id}"] .gtr-name`);
      if (nameEl) startEdit(nameEl, newItem);
    });
  }

  function deleteItem(id) {
    const item = findInTree(project.items, id);
    if (!item) return;
    if (item.children.length > 0 &&
        !confirm(`Delete "${item.name}" and all its children?`)) return;
    removeFromTree(project.items, id);
    if (selId    === id) selId    = null;
    if (detailId === id) detailId = null;
    scheduleSave();
    render();
  }

  function cycleState(item) {
    item.state = STATE_NEXT[item.state] || 'ready';
    scheduleSave();
    render();
  }

  // ── Inline name editing ───────────────────────────────────────────────────────

  function startEdit(nameEl, item) {
    nameEl.contentEditable = 'true';
    nameEl.focus();
    const range = document.createRange();
    range.selectNodeContents(nameEl);
    window.getSelection().removeAllRanges();
    window.getSelection().addRange(range);

    function commit() {
      nameEl.contentEditable = 'false';
      const val = nameEl.textContent.trim();
      if (val && val !== item.name) {
        item.name = val;
        scheduleSave();
        render();
      } else {
        nameEl.textContent = item.name;
      }
    }
    nameEl.addEventListener('blur', commit, { once: true });
    nameEl.addEventListener('keydown', e => {
      if (e.key === 'Enter')  { e.preventDefault(); nameEl.blur(); }
      if (e.key === 'Escape') { nameEl.textContent = item.name; nameEl.blur(); }
    }, { once: true });
  }

  // ── Render ────────────────────────────────────────────────────────────────────

  function render() {
    if (!project) {
      hdr.style.display = 'none';
      treeEl.innerHTML  = '';
      const empty = document.createElement('div');
      empty.className   = 'gtr-empty';
      empty.textContent = 'enter a project path above to open or create a project';
      treeEl.appendChild(empty);
      return;
    }

    hdr.style.display    = 'flex';
    projNameEl.textContent = project.name;
    projPathEl.textContent  = projectPath;

    // Summary pills
    summaryEl.innerHTML = '';
    const counts = { ready:0, 'in-progress':0, 'post-production':0, 'can-ship':0 };
    function countLeaf(n) {
      if (!n.children.length) counts[n.state]++;
      else n.children.forEach(countLeaf);
    }
    project.items.forEach(countLeaf);
    Object.entries(counts).forEach(([s, c]) => {
      if (!c) return;
      const p = document.createElement('div');
      p.className = 'gtr-pill ' + STATE_PILL[s];
      p.innerHTML = `<span class="gtr-pill-dot" style="background:${STATE_COLOR[s]}"></span>${c} ${STATE_LABEL[s]}`;
      summaryEl.appendChild(p);
    });

    // Tree
    treeEl.innerHTML = '';
    if (!project.items.length) {
      const empty = document.createElement('div');
      empty.className   = 'gtr-empty';
      empty.textContent = 'no items yet — click + to add one';
      treeEl.appendChild(empty);
    } else {
      project.items.forEach(item => treeEl.appendChild(renderItem(item, 0)));
    }
  }

  function renderItem(item, depth) {
    const hasKids = item.children.length > 0;
    const isOpen  = !collapsed.has(item.id);

    const wrapper = document.createElement('div');
    wrapper.dataset.id = item.id;

    // ── Row ─────────────────────────────────────────────────────
    const row = document.createElement('div');
    row.className = 'gtr-row' + (selId === item.id ? ' gtr-sel' : '');

    const left = document.createElement('div');
    left.className = 'gtr-left';

    const tog = document.createElement('div');
    tog.className = 'gtr-tog' + (hasKids ? '' : ' leaf');
    tog.style.marginLeft = (0.5 + depth * 1.0) + 'rem';
    tog.textContent = hasKids ? (isOpen ? '▾' : '▸') : '';

    const dotState = hasKids ? rolledState(item) : item.state;
    const dot = document.createElement('div');
    dot.className = 'gtr-dot';
    dot.style.background = STATE_COLOR[dotState];
    dot.title = STATE_LABEL[dotState] + (hasKids ? ' (derived from children)' : ' — click to advance');
    dot.addEventListener('click', e => { e.stopPropagation(); if (!hasKids) cycleState(item); });

    const nameEl = document.createElement('span');
    nameEl.className = 'gtr-name';
    nameEl.textContent = item.name;
    nameEl.addEventListener('dblclick', e => { e.stopPropagation(); startEdit(nameEl, item); });

    left.appendChild(tog);
    left.appendChild(dot);
    left.appendChild(nameEl);

    const right = document.createElement('div');
    right.className = 'gtr-right';

    const r    = rollup(item);
    const over = r.used > r.budget && r.budget > 0;
    const budget = document.createElement('div');
    budget.className = 'gtr-budget';
    budget.innerHTML =
      `<span class="${over ? 'b-over' : 'b-used'}">${r.used}</span>` +
      `<span class="b-sep"> / </span>` +
      `<span class="b-total">${r.budget} hrs</span>`;

    const addBtn = document.createElement('button');
    addBtn.className = 'gtr-add-btn';
    addBtn.title = hasKids ? 'add child item' : 'add sibling';
    addBtn.textContent = '+';
    addBtn.addEventListener('click', e => {
      e.stopPropagation();
      if (hasKids) addChild(item);
      else         addSibling(item);
    });

    right.appendChild(budget);
    right.appendChild(addBtn);

    row.appendChild(left);
    row.appendChild(right);

    row.addEventListener('click', () => {
      const wasSel = selId === item.id;
      selId = item.id;
      if (hasKids) {
        if (isOpen) collapsed.add(item.id);
        else        collapsed.delete(item.id);
        if (detailId === item.id) detailId = null;
      } else {
        detailId = (detailId === item.id && wasSel) ? null : item.id;
      }
      render();
    });

    wrapper.appendChild(row);

    // ── Detail panel ─────────────────────────────────────────────
    if (!hasKids && detailId === item.id) {
      wrapper.appendChild(buildDetail(item));
    }

    // ── Children ──────────────────────────────────────────────────
    if (hasKids && isOpen) {
      const kids = document.createElement('div');
      kids.className = 'gtr-kids open';
      item.children.forEach(c => kids.appendChild(renderItem(c, depth + 1)));
      wrapper.appendChild(kids);
    }

    return wrapper;
  }

  function buildDetail(item) {
    const panel = document.createElement('div');
    panel.className = 'gtr-detail';
    panel.addEventListener('click', e => e.stopPropagation());

    function textField(label, value, onchange) {
      const row = document.createElement('div');
      row.className = 'gtr-dr';
      const lbl = document.createElement('span');
      lbl.className = 'gtr-dl';
      lbl.textContent = label;
      const inp = document.createElement('input');
      inp.className = 'gtr-dinput';
      inp.type = 'text';
      inp.value = value;
      inp.addEventListener('keydown', e => { if (e.key === 'Enter') inp.blur(); });
      inp.addEventListener('blur', () => { onchange(inp.value); scheduleSave(); render(); });
      row.appendChild(lbl);
      row.appendChild(inp);
      panel.appendChild(row);
    }

    function numField(label, value, onchange) {
      const row = document.createElement('div');
      row.className = 'gtr-dr';
      const lbl = document.createElement('span');
      lbl.className = 'gtr-dl';
      lbl.textContent = label;
      const inp = document.createElement('input');
      inp.className = 'gtr-dinput';
      inp.type  = 'number';
      inp.min   = '0';
      inp.step  = '0.5';
      inp.value = value;
      inp.addEventListener('keydown', e => { if (e.key === 'Enter') inp.blur(); });
      inp.addEventListener('blur', () => {
        const n = parseFloat(inp.value);
        if (!isNaN(n) && n >= 0) { onchange(n); scheduleSave(); render(); }
      });
      row.appendChild(lbl);
      row.appendChild(inp);
      panel.appendChild(row);
    }

    textField('assigned', item.assignee, v => { item.assignee = v.trim(); });
    numField('budget hrs', item.budget, v => { item.budget = v; });
    numField('logged hrs', item.used,   v => { item.used   = v; });

    // State selector
    const stRow = document.createElement('div');
    stRow.className = 'gtr-dr';
    const stLbl = document.createElement('span');
    stLbl.className = 'gtr-dl';
    stLbl.textContent = 'state';
    const sel = document.createElement('select');
    sel.className = 'gtr-dselect';
    Object.entries(STATE_LABEL).forEach(([k, v]) => {
      const opt = document.createElement('option');
      opt.value = k; opt.textContent = v;
      if (k === item.state) opt.selected = true;
      sel.appendChild(opt);
    });
    sel.addEventListener('change', () => { item.state = sel.value; scheduleSave(); render(); });
    stRow.appendChild(stLbl);
    stRow.appendChild(sel);
    panel.appendChild(stRow);

    // Notes
    const notesRow = document.createElement('div');
    notesRow.className = 'gtr-dr';
    notesRow.style.alignItems = 'flex-start';
    const notesLbl = document.createElement('span');
    notesLbl.className = 'gtr-dl';
    notesLbl.style.paddingTop = '0.2rem';
    notesLbl.textContent = 'notes';
    const ta = document.createElement('textarea');
    ta.className = 'gtr-dtextarea';
    ta.rows  = 2;
    ta.value = item.desc || '';
    ta.addEventListener('blur', () => { item.desc = ta.value; scheduleSave(); });
    notesRow.appendChild(notesLbl);
    notesRow.appendChild(ta);
    panel.appendChild(notesRow);

    // Delete button
    const delRow = document.createElement('div');
    delRow.className = 'gtr-dr';
    delRow.style.justifyContent = 'flex-end';
    delRow.style.paddingTop = '0.15rem';
    const delBtn = document.createElement('button');
    delBtn.className = 'gtr-pb-btn';
    delBtn.style.cssText = 'color:#e05555;border-color:#e05555;';
    delBtn.textContent = 'delete item';
    delBtn.addEventListener('click', () => deleteItem(item.id));
    delRow.appendChild(delBtn);
    panel.appendChild(delRow);

    return panel;
  }

  // ── Init ──────────────────────────────────────────────────────────────────────
  render();
  if (projectPath) openProject(projectPath);

  return {
    el,
    focus() { pathInput.focus(); },
    destroy() {
      if (saveTimer) {
        clearTimeout(saveTimer);
        save(); // flush any pending save on close
      }
    },
  };
}