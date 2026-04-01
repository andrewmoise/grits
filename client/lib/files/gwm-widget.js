/*
 * @cell files-widget
 * @version 0.2
 * @about
 *   File browser for the Gimbal shell. Lazily loads directory children
 *   on expand. Expand/collapse state is preserved across collapses.
 * @implements gimbal-shell#widget
 */

const SVG_CARET = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor"
  stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"
  style="width:1.2em;height:1.2em;display:block;flex-shrink:0;
         transition:transform calc(var(--dur) * 0.5) var(--ease-sine);
         transform-origin:center;">
  <polyline points="9 18 15 12 9 6"/>
</svg>`;

const STYLE_ID = 'gimbal-files-styles';

function injectStyles() {
  if (document.getElementById(STYLE_ID)) return;
  const s = document.createElement('style');
  s.id = STYLE_ID;
  s.textContent = `
    .gf-tree {
      padding: 0.375rem 0;
      font-family: 'JetBrains Mono', 'IBM Plex Mono', monospace;
      font-size: 0.75rem;
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
    .gf-caret.blank     { visibility: hidden; }   /* file indentation spacer */

    .gf-name { flex: 1; overflow: hidden; text-overflow: ellipsis; }
    .gf-name.is-dir  { color: var(--text-hi); }
    .gf-name.is-file { color: var(--text); }

    .gf-children { display: none; }
    .gf-children.open { display: block; }

    .gf-loading {
      padding: 0.2rem 0.5rem;
      color: var(--text-dim);
      font-size: 0.7rem;
      font-family: 'JetBrains Mono', monospace;
    }

    .gf-error {
      padding: 0.2rem 0.5rem;
      color: var(--red);
      font-size: 0.7rem;
      font-family: 'JetBrains Mono', monospace;
    }
  `;
  document.head.appendChild(s);
}

// ── Node state ────────────────────────────────────────────
// Each node in the tree carries:
//   file     : GritsFile
//   name     : string
//   open     : boolean          (dirs only)
//   loaded   : boolean          (dirs only — children fetched at least once)
//   children : Map<name, node>  (dirs only, populated on first expand)
//   el       : { row, caretEl, childrenEl }  — live DOM refs once built

function makeNode(name, file) {
  return {
    name,
    file,
    open:     false,
    loaded:   false,
    children: null,   // Map<name, node>, null until first expand
    el:       null,
  };
}

// ── Build a row element for a node ────────────────────────
function buildRow(node, depth, onSelect, onToggle) {
  const isDir = node.file.isDir();

  const row = document.createElement('div');
  row.className = 'gf-row';
  row.style.paddingLeft = `${0.5 + depth * 1.1}rem`;

  // Caret (dirs) or blank spacer (files)
  const caretEl = document.createElement('span');
  caretEl.className = `gf-caret${isDir ? '' : ' blank'}`;
  if (isDir) caretEl.innerHTML = SVG_CARET;
  row.appendChild(caretEl);

  // Name
  const nameEl = document.createElement('span');
  nameEl.className = `gf-name ${isDir ? 'is-dir' : 'is-file'}`;
  nameEl.textContent = node.name;
  row.appendChild(nameEl);

  // Children container (dirs only)
  let childrenEl = null;
  if (isDir) {
    childrenEl = document.createElement('div');
    childrenEl.className = 'gf-children';
  }

  node.el = { row, caretEl, childrenEl };

  row.addEventListener('click', () => {
    if (isDir) onToggle(node);
    else       onSelect(node);
  });

  return { row, childrenEl };
}

// ── Widget factory ────────────────────────────────────────
export default function createWidget({ name, evalContext = {} }) {
  injectStyles();

  const el = document.createElement('div');
  el.className = 'gf-tree';
  el.style.cssText = 'overflow:auto;flex:1;min-height:0;height:100%;';

  const gg     = evalContext.fs;
  const vol    = gg.volume(window.location.origin, 'client');
  let selected = null;

  // ── Toggle open/closed ──────────────────────────────────
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

    // node.open is true from here
    caretEl.classList.add('open');
    childrenEl.classList.add('open');
    
    if (node.open && !node.loaded) {
      // First expand — fetch children
      node.loaded = true;
      childrenEl.appendChild(msgEl('gf-loading', '...', depthOf(node)+1))

      try {
        const childFiles = await node.file.children();   // Map<name, GritsFile>
        node.children = new Map();
        childrenEl.innerHTML = '';

        // Sort: dirs first, then files, both alphabetically
        const sorted = [...childFiles.entries()].sort(([an, af], [bn, bf]) => {
          const ad = af.isDir(), bd = bf.isDir();
          if (ad !== bd) return ad ? -1 : 1;
          return an.localeCompare(bn);
        });

        for (const [childName, childFile] of sorted) {
          const childNode = makeNode(childName, childFile);
          node.children.set(childName, childNode);
          const depth = depthOf(node) + 1;
          const { row, childrenEl: grandchildrenEl } = buildRow(
            childNode, depth, onSelect, toggle
          );
          childrenEl.appendChild(row);
          if (grandchildrenEl) childrenEl.appendChild(grandchildrenEl);
        }

        if (sorted.length === 0) {
          childrenEl.appendChild(msgEl('gf-loading', '(empty)', depthOf(node)+1));
        }
      } catch (e) {
        childrenEl.innerHTML = `<div class="gf-error">${e.message}</div>`;
        node.loaded = false;   // allow retry
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

  // ── Depth helper ────────────────────────────────────────
  // Since we don't store parent refs we compute depth from DOM instead.
  function depthOf(node) {
    let el = node.el?.row;
    let depth = 0;
    while (el) {
      if (el.classList?.contains('gf-children')) depth++;
      el = el.parentElement;
    }
    return depth;
  }

  // ── Initial load ────────────────────────────────────────
  async function loadRoot() {
    try {
      const rootFile = await vol.lookup('/');
      const rootNode = makeNode('/', rootFile);

      // Build a synthetic open root — we show its children directly,
      // not the root row itself, to avoid a pointless top-level entry.
      rootNode.open   = true;
      rootNode.loaded = true;

      const childFiles = await rootFile.children();
      rootNode.children = new Map();

      const sorted = [...childFiles.entries()].sort(([an, af], [bn, bf]) => {
        const ad = af.isDir(), bd = bf.isDir();
        if (ad !== bd) return ad ? -1 : 1;
        return an.localeCompare(bn);
      });

      for (const [childName, childFile] of sorted) {
        const childNode = makeNode(childName, childFile);
        rootNode.children.set(childName, childNode);
        const { row, childrenEl } = buildRow(childNode, 0, onSelect, toggle);
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

  loadRoot();

  el.tabIndex = 0;
  el.style.outline = 'none';
  return { el, focus() { el.focus(); }, destroy() {} };
}