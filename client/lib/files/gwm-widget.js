/*
 * @cell files-widget
 * @version 0.3
 * @about
 *   File browser for the Gimbal shell. Lazily loads directory children
 *   on expand. Expand/collapse state is preserved across collapses.
 * @implements gimbal-shell#widget
 */

import { FONT_MONO, injectStyles } from '../style/style.js';
import { toast } from '../gimbal/dialog.js';

const SVG_CARET = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor"
  stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"
  style="width:1.2em;height:1.2em;display:block;flex-shrink:0;
         transition:transform calc(var(--dur) * 0.5) var(--ease-sine);
         transform-origin:center;">
  <polyline points="9 18 15 12 9 6"/>
</svg>`;

const STYLE_ID = 'gimbal-files-styles';

function ensureStyles() {
  injectStyles(STYLE_ID, `
    .gf-tree {
      padding: 0.375rem 0;
      font-family: ${FONT_MONO};
      font-size: var(--fs-base, 0.75rem);
      line-height: 1.5;
      user-select: none;
    }

    .gf-row {
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
    .gf-row:hover    { background: var(--bg-hover); color: var(--text-hi); }
    .gf-row.selected { background: var(--bg-active); color: var(--text-hi); }

    .gf-caret {
      display: flex; align-items: center; justify-content: center;
      width: 1.2em; flex-shrink: 0; color: var(--text-dim);
    }
    .gf-caret.open svg  { transform: rotate(90deg); }
    .gf-caret.blank     { visibility: hidden; }

    .gf-name { flex: 1; overflow: hidden; text-overflow: ellipsis; }
    .gf-name.is-dir  { color: var(--text-hi); }
    .gf-name.is-file { color: var(--text); }

    .gf-children { display: none; }
    .gf-children.open { display: block; }

    .gf-loading {
      padding: 0.2rem 0.5rem;
      color: var(--text-dim);
      font-size: var(--fs-sm, 0.70rem);
      font-family: ${FONT_MONO};
    }

    .gf-error {
      padding: 0.2rem 0.5rem;
      color: var(--red);
      font-size: var(--fs-sm, 0.70rem);
      font-family: ${FONT_MONO};
    }
  `);
}

// ── Node state ────────────────────────────────────────────
function makeNode(name, file, parentPath = '') {
  // fullPath is always relative to widget root, no leading slash
  const fullPath = parentPath
    ? (name ? `${parentPath}/${name}` : parentPath)
    : (name || '');
  return {
    name,
    fullPath,
    file,
    open:     false,
    loaded:   false,
    children: null,
    el:       null,
  };
}

// ── Build a row element for a node ────────────────────────
function buildRow(node, depth, onSelect, onToggle, onOpenFile) {
  const isDir = node.file.isDir();

  const row = document.createElement('div');
  row.className = 'gf-row';
  row.style.paddingLeft = `${0.5 + depth * 1.1}rem`;

  const caretEl = document.createElement('span');
  caretEl.className = `gf-caret${isDir ? '' : ' blank'}`;
  if (isDir) caretEl.innerHTML = SVG_CARET;
  row.appendChild(caretEl);

  const nameEl = document.createElement('span');
  nameEl.className = `gf-name ${isDir ? 'is-dir' : 'is-file'}`;
  nameEl.textContent = node.name;
  row.appendChild(nameEl);

  let childrenEl = null;
  if (isDir) {
    childrenEl = document.createElement('div');
    childrenEl.className = 'gf-children';
  }

  row.dataset.cid = node.file.cid();
  node.el = { row, caretEl, childrenEl };

  row.addEventListener('click', () => {
    if (isDir) onToggle(node);
    else       onSelect(node);
  });
  row.addEventListener('dblclick', async () => {
    if (!isDir) await onOpenFile(node);
  });

  return { row, childrenEl };
}

// ── Widget factory ────────────────────────────────────────
import { GimbalPath } from '../gimbal/path.js';

export default function createWidget({ name, gimbal, path: gimbalPath, payload = null, args = [] }) {
  ensureStyles();

  const el = document.createElement('div');
  el.className = 'gf-tree';
  el.style.cssText = 'overflow:auto;flex:1;min-height:0;height:100%;';

  if (payload != null && !(payload instanceof GimbalPath))
    throw new Error(`files: expected a directory path (GimbalPath), got ${payload?.constructor?.name ?? typeof payload}`);

  // These define the hard root for this widget instance
  let serverUrl = gimbal?._serverUrl || window.location.origin;
  let volume    = 'primary';
  let basePath  = '';
  let selected = null;
  let controls = null;
  let rootNode = null;
  let domBusy = 0;
  let refreshTimer = null;

  if (gimbalPath && gimbal) {
    const r = gimbal.resolvePath(gimbalPath.abs());
    volume   = r.volumeName;
    basePath = r.path.replace(/^\//, '');
  } else if (args && args.length === 1) {
    const a = args[0];
    let p = null;
    if (typeof a === 'string') p = a;
    else if (a && typeof a === 'object' && typeof a.path === 'string') p = a.path;

    if (p != null && gimbal) {
      try {
        const r = gimbal.resolvePath(p);
        volume   = r.volumeName;
        basePath = r.path.replace(/^\//, '');
      } catch (e) {
        console.warn('[files] failed to resolve path, using defaults');
      }
    }
  }

  function makeTitle(p) {
    return p ? `/${p}/` : '/';
  }

  const atRoot = !basePath || !basePath.includes('/');

  const parentBtn = {
    icon:    'parentDir',
    label:   'parent directory',
    enabled: !atRoot,
    action() { goToParent(); },
  };

  const decoration = { title: makeTitle(basePath), leftButtons: [parentBtn] };

  const vol = gimbal.grits.volume(gimbal._serverUrl, volume);

  async function toggle(node) {
    if (!node.file.isDir()) return;

    node.open = !node.open;
    const { caretEl, childrenEl } = node.el;

    if (!node.open) {
      const dur = parseFloat(getComputedStyle(document.documentElement)
        .getPropertyValue('--dur')) * 1000 * 0.5;
      caretEl.classList.remove('open');
      setTimeout(() => childrenEl.classList.remove('open'), dur);
      return;
    }

    caretEl.classList.add('open');
    childrenEl.classList.add('open');

    if (!node.loaded) {
      node.loaded = true;
      childrenEl.appendChild(msgEl('gf-loading', '...', depthOf(node) + 1));
      domBusy++;

      try {
        const childFiles = await node.file.children();
        node.children = new Map();
        childrenEl.innerHTML = '';

        const sorted = [...childFiles.entries()].sort(([an, af], [bn, bf]) => {
          const ad = af.isDir(), bd = bf.isDir();
          if (ad !== bd) return ad ? -1 : 1;
          return an.localeCompare(bn);
        });

        for (const [childName, childFile] of sorted) {
          const childNode = makeNode(childName, childFile, node.fullPath);
          node.children.set(childName, childNode);
          const depth = depthOf(node) + 1;
          const { row, childrenEl: grandchildrenEl } = buildRow(
            childNode, depth, onSelect, toggle, onOpenFile
          );
          childrenEl.appendChild(row);
          if (grandchildrenEl) childrenEl.appendChild(grandchildrenEl);
        }

        if (sorted.length === 0) {
          childrenEl.appendChild(msgEl('gf-loading', '(empty)', depthOf(node) + 1));
        }
      } catch (e) {
        childrenEl.innerHTML = `<div class="gf-error">${e.message}</div>`;
        node.loaded = false;
      } finally {
        domBusy--;
      }
    }
  }

  function msgEl(cls, text, depth) {
    const d = document.createElement('div');
    d.className = cls;
    d.style.paddingLeft = `${0.5 + depth * 1.1}rem`;
    d.textContent = text;
    return d;
  }

  function onSelect(node) {
    if (selected?.el?.row) selected.el.row.classList.remove('selected');
    selected = node;
    node.el.row.classList.add('selected');
  }

  async function onOpenFile(node) {
    if (!gimbal) { console.warn('[files] no gimbal, cannot open file'); return; }

    const rel = node.fullPath || '';
    const fullPath = basePath
      ? (rel ? `${basePath}/${rel}` : basePath)
      : rel;

    const gp = gimbal.p('//' + volume + '/' + fullPath);
    await gp.launch('edit');
  }

  function depthOf(node) {
    let el = node.el?.row;
    let depth = 0;
    while (el) {
      if (el.classList?.contains('gf-children')) depth++;
      el = el.parentElement;
    }
    return depth;
  }

  function updateParentBtnState() {
  const atRoot = !basePath;
    if (parentBtn._el) {
      parentBtn._el.disabled = atRoot;
      parentBtn._el.style.opacity = atRoot ? '0.3' : '1';
    }
  }

  async function goToParent() {
    if (!basePath) return;

    const parent = basePath.split('/').slice(0, -1).join('/');

    try {
      await vol.lookup(parent);
    } catch (e) {
      toast(`Cannot open parent: ${e.message}`);
      return;
    }

    domBusy++;
    try {
      rootNode = null;
      basePath = parent;
      decoration.title = makeTitle(basePath);
      controls?.setTitle(decoration.title);
      await loadRoot();
      updateParentBtnState();
    } finally {
      domBusy--;
    }
  }

  async function collectChanges(containerEl, parentNode, parentPath, newChildrenMap) {
    const ops = [];
    const oldChildren = parentNode.children;

    for (const [name] of oldChildren) {
      if (!newChildrenMap.has(name)) {
        ops.push({ type: 'remove', parentNode, node: oldChildren.get(name) });
      }
    }

    for (const [name, newFile] of newChildrenMap) {
      if (oldChildren.has(name)) {
        const node = oldChildren.get(name);
        ops.push({ type: 'update', node, newFile });
        if (newFile.isDir() && node.open && node.loaded && node.el.childrenEl) {
          const grandChildren = await newFile.children();
          const childOps = await collectChanges(node.el.childrenEl, node,
            parentPath ? `${parentPath}/${name}` : name, grandChildren);
          ops.push(...childOps);
        }
      } else {
        ops.push({ type: 'add', parentNode, name, file: newFile, parentPath, containerEl });
      }
    }

    return ops;
  }

  function applyChanges(ops) {
    for (const op of ops) {
      if (op.type === 'remove') {
        op.node.el.row.remove();
        if (op.node.el.childrenEl) op.node.el.childrenEl.remove();
        op.parentNode.children.delete(op.node.name);
      }
    }

    for (const op of ops) {
      if (op.type === 'update') {
        op.node.file = op.newFile;
        op.node.el.row.dataset.cid = op.newFile.cid();
      }
    }

    for (const op of ops) {
      if (op.type === 'add') {
        const newNode = makeNode(op.name, op.file, op.parentPath);
        const { row, childrenEl } = buildRow(newNode, 0, onSelect, toggle, onOpenFile);

        const nextSibling = [...op.parentNode.children.entries()]
          .find(([siblingName]) => siblingName > op.name);

        if (nextSibling) {
          op.containerEl.insertBefore(row, nextSibling[1].el.row);
          if (childrenEl) op.containerEl.insertBefore(childrenEl, nextSibling[1].el.row);
        } else {
          op.containerEl.appendChild(row);
          if (childrenEl) op.containerEl.appendChild(childrenEl);
        }

        op.parentNode.children.set(op.name, newNode);
      }
    }
  }

  async function reload() {
    if (!rootNode) { await loadRoot(); return; }

    domBusy++;
    const oldScroll = el.scrollTop;
    try {
      const rootFile = await vol.lookup(basePath);
      const newChildren = await rootFile.children();
      rootNode.file = rootFile;
      const ops = await collectChanges(el, rootNode, '', newChildren);
      if (domBusy > 1) return;
      applyChanges(ops);
      el.scrollTop = oldScroll;
      updateParentBtnState();
    } catch (e) {
      toast(`Reload failed: ${e.message}`);
    } finally {
      domBusy--;
    }
  }

  async function refreshTick() {
    if (!rootNode) return;
    try {
      const rootFile = await vol.lookup(basePath);
      if (rootFile.cid() === rootNode.file.cid()) return;
      const newChildren = await rootFile.children();
      rootNode.file = rootFile;
      const ops = await collectChanges(el, rootNode, '', newChildren);
      if (domBusy > 0) return;
      const oldScroll = el.scrollTop;
      applyChanges(ops);
      el.scrollTop = oldScroll;
      updateParentBtnState();
    } catch (e) {
      // silently ignore — retry next tick
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

  async function loadRoot() {
    try {
      const rootFile = await vol.lookup(basePath);
      const displayName = basePath ? basePath.split('/').pop() : '/';

      rootNode = makeNode('', rootFile, '');
      rootNode.open   = true;
      rootNode.loaded = true;

      const childFiles = await rootFile.children();
      rootNode.children = new Map();
      el.innerHTML = '';

      const sorted = [...childFiles.entries()].sort(([an, af], [bn, bf]) => {
        const ad = af.isDir(), bd = bf.isDir();
        if (ad !== bd) return ad ? -1 : 1;
        return an.localeCompare(bn);
      });

      for (const [childName, childFile] of sorted) {
        const childNode = makeNode(childName, childFile, '');
        rootNode.children.set(childName, childNode);
        const { row, childrenEl } = buildRow(childNode, 0, onSelect, toggle, onOpenFile);
        el.appendChild(row);
        if (childrenEl) el.appendChild(childrenEl);
      }

      if (sorted.length === 0) {
        el.appendChild(msgEl('gf-loading', '(empty)', 0));
      }
    } catch (e) {
      const err = document.createElement('div');
      err.className = 'gf-error';
      err.textContent = e.message;
      el.appendChild(err);
    }
  }

  loadRoot().then(startAutoRefresh);

  el.tabIndex = 0;
  el.style.outline = 'none';
  return {
    el,
    decoration,
    get controls() { return controls; },
    set controls(c) { controls = c; },
    focus() { el.focus(); },
    destroy() {
      stopAutoRefresh();
    },
  };
}
