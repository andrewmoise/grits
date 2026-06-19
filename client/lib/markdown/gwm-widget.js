/*
 * @cell markdown-widget  v1.0
 * @about
 *   Renders Markdown content to styled DOM using @lezer/markdown.
 *   Supports GFM tables, task lists, and strikethrough.
 *   Read-only — no edit support.
 * @implements gimbal-shell#widget
 */

const STYLE_ID = 'gimbal-markdown-styles';
const MD_ICON = {
  svg: `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round"><path d="M9.5 4H14.5V13H18L12 20L6 13H9.5Z" fill="currentColor" stroke="none"/></svg>`,
};

function ensureStyles() {
  if (document.getElementById(STYLE_ID)) return;
  const s = document.createElement('style');
  s.id = STYLE_ID;
  s.textContent = `
    .gm-wrap {
      width: 100%; height: 100%;
      overflow-y: auto; overflow-x: hidden;
      box-sizing: border-box;
      scrollbar-width: thin;
      scrollbar-color: var(--border-hi) transparent;
    }
    .gm-wrap::-webkit-scrollbar        { width: 0.25rem; }
    .gm-wrap::-webkit-scrollbar-track  { background: transparent; }
    .gm-wrap::-webkit-scrollbar-thumb  { background: var(--border-hi); border-radius: 0.125rem; }

    .gm-body {
      max-width: 52rem;
      margin: 0 auto;
      padding: 2rem 2.5rem 4rem;
      font-family: var(--font-ui);
      font-size: var(--fs-md);
      line-height: 1.75;
      color: var(--text);
    }

    /* ── Headings ────────────────────────────────── */
    .gm-body h1 {
      font-family: var(--font-heading);
      font-size: 1.6rem;
      font-weight: 600;
      color: var(--text-hi);
      margin: 0 0 0.5rem 0;
      padding-bottom: 0.35rem;
      border-bottom: 2px solid var(--border);
      line-height: 1.3;
    }
    .gm-body h2 {
      font-family: var(--font-heading);
      font-size: 1.25rem;
      font-weight: 600;
      color: var(--text-hi);
      margin: 1.6rem 0 0.4rem 0;
      padding-bottom: 0.25rem;
      border-bottom: 1px solid var(--border);
      line-height: 1.35;
    }
    .gm-body h3 {
      font-family: var(--font-heading);
      font-size: 1.05rem;
      font-weight: 600;
      color: var(--text-hi);
      margin: 1.4rem 0 0.3rem 0;
      line-height: 1.4;
    }
    .gm-body h4 {
      font-family: var(--font-heading);
      font-size: 0.95rem;
      font-weight: 600;
      color: var(--text-hi);
      margin: 1.2rem 0 0.25rem 0;
    }
    .gm-body h5 {
      font-family: var(--font-heading);
      font-size: 0.875rem;
      font-weight: 600;
      color: var(--text);
      margin: 1rem 0 0.2rem 0;
    }
    .gm-body h6 {
      font-family: var(--font-heading);
      font-size: 0.8125rem;
      font-weight: 600;
      color: var(--text-dim);
      margin: 1rem 0 0.2rem 0;
    }

    /* ── Paragraphs & text ───────────────────────── */
    .gm-body p {
      margin: 0 0 0.75rem 0;
    }
    .gm-body p:last-child {
      margin-bottom: 0;
    }

    /* ── Links ────────────────────────────────────── */
    .gm-body a {
      color: var(--a1);
      text-decoration: none;
      transition: color 0.12s;
    }
    .gm-body a:hover {
      color: var(--blue-hi);
      text-decoration: underline;
    }

    /* ── Inline code ─────────────────────────────── */
    .gm-body code {
      font-family: var(--font-mono);
      font-size: 0.78em;
      background: var(--bg-elevated);
      color: var(--amber);
      padding: 0.12em 0.4em;
      border-radius: 0.3rem;
    }
    .gm-body pre {
      margin: 0.6rem 0 1rem;
      background: var(--bg-root);
      border-radius: 0.5rem;
      border-left: 3px solid var(--a1-dim);
      overflow-x: auto;
      scrollbar-width: thin;
      scrollbar-color: var(--border-hi) transparent;
    }
    .gm-body pre::-webkit-scrollbar        { width: 0.25rem; height: 0.25rem; }
    .gm-body pre::-webkit-scrollbar-track  { background: transparent; }
    .gm-body pre::-webkit-scrollbar-thumb  { background: var(--border-hi); border-radius: 0.125rem; }
    .gm-body pre code {
      background: none;
      color: var(--text);
      padding: 0.75rem 1rem;
      display: block;
      font-size: 0.78em;
      line-height: 1.55;
    }

    /* ── Blockquotes ─────────────────────────────── */
    .gm-body blockquote {
      margin: 0.6rem 0 0.9rem;
      padding: 0.25rem 0.75rem 0.25rem 1rem;
      border-left: 3px solid var(--a1-dim);
      color: var(--text-dim);
    }
    .gm-body blockquote p {
      margin: 0.25rem 0;
    }

    /* ── Lists ───────────────────────────────────── */
    .gm-body ul, .gm-body ol {
      margin: 0.3rem 0 0.75rem;
      padding-left: 1.5rem;
    }
    .gm-body li {
      margin: 0.1rem 0;
    }
    .gm-body li > p {
      margin: 0.15rem 0;
    }
    .gm-body li > ul, .gm-body li > ol {
      margin: 0.1rem 0;
    }

    /* ── Task lists ──────────────────────────────── */
    .gm-task {
      display: flex;
      align-items: center;
      gap: 0.4rem;
    }
    .gm-task input[type="checkbox"] {
      appearance: none;
      -webkit-appearance: none;
      width: 0.85rem; height: 0.85rem;
      border: 1.5px solid var(--text-dim);
      border-radius: 0.2rem;
      flex-shrink: 0;
      cursor: default;
      position: relative;
      transition: border-color 0.1s, background 0.1s;
    }
    .gm-task input[type="checkbox"]:checked {
      background: var(--a1);
      border-color: var(--a1);
    }
    .gm-task input[type="checkbox"]:checked::after {
      content: '';
      position: absolute;
      left: 0.2rem; top: 0.03rem;
      width: 0.3rem; height: 0.5rem;
      border: solid #fff;
      border-width: 0 1.5px 1.5px 0;
      transform: rotate(45deg);
    }
    .gm-task .gm-task-text {
      flex: 1;
    }
    .gm-task .gm-task-text.done {
      text-decoration: line-through;
      color: var(--text-dim);
    }

    /* ── Horizontal rules ────────────────────────── */
    .gm-body hr {
      border: none;
      border-top: 1px solid var(--border);
      margin: 1.2rem 0;
    }

    /* ── Tables ──────────────────────────────────── */
    .gm-body table {
      border-collapse: collapse;
      margin: 0.6rem 0 1rem;
      font-size: 0.8125rem;
      width: 100%;
    }
    .gm-body th, .gm-body td {
      border: 1px solid var(--border);
      padding: 0.35rem 0.6rem;
      text-align: left;
    }
    .gm-body th {
      background: var(--bg-elevated);
      color: var(--text-hi);
      font-weight: 500;
    }
    .gm-body tr:nth-child(even) td {
      background: var(--bg-hover);
    }

    /* ── Images ──────────────────────────────────── */
    .gm-body img {
      max-width: 100%;
      height: auto;
      border-radius: 0.4rem;
      margin: 0.5rem 0;
    }

    /* ── Strikethrough ───────────────────────────── */
    .gm-body s, .gm-body del {
      color: var(--text-dim);
    }

    /* ── Code info label ─────────────────────────── */
    .gm-lang {
      display: inline-block;
      font-family: var(--font-mono);
      font-size: 0.65rem;
      color: var(--text-dim);
      padding: 0.15rem 0.75rem 0;
    }

    /* ── Emphasis ────────────────────────────────── */
    .gm-body em {
      font-style: italic;
    }
    .gm-body strong {
      font-weight: 600;
      color: var(--text-hi);
    }

    /* ── Empty state ─────────────────────────────── */
    .gm-empty {
      display: flex;
      align-items: center;
      justify-content: center;
      width: 100%; height: 100%;
      color: var(--text-dim);
      font-family: var(--font-ui);
      font-size: var(--fs-base);
      opacity: 0.6;
    }
  `;
  document.head.appendChild(s);
}

const SKIP = new Set([
  'HeaderMark', 'QuoteMark', 'ListMark', 'EmphasisMark',
  'CodeMark', 'LinkMark', 'URL', 'CodeInfo',
  'StrikethroughMark', 'TableDelimiter',
]);

function attachChildren(parent, node, src) {
  let pos = node.from;
  let child = node.firstChild;
  while (child) {
    if (child.from > pos) {
      parent.appendChild(document.createTextNode(src.slice(pos, child.from)));
    }
    const el = renderNode(child, src);
    if (el) parent.appendChild(el);
    pos = child.to;
    child = child.nextSibling;
  }
  if (pos < node.to) {
    parent.appendChild(document.createTextNode(src.slice(pos, node.to)));
  }
}

function firstChildNamed(node, name) {
  let c = node.firstChild;
  while (c) {
    if (c.type.name === name) return c;
    c = c.nextSibling;
  }
  return null;
}

function renderNode(node, src) {
  const name = node.type.name;
  if (SKIP.has(name)) return null;

  switch (name) {

    /* ── Headings ─────────────────────────────── */
    case 'ATXHeading1':
    case 'SetextHeading1': {
      const h = document.createElement('h1');
      attachChildren(h, node, src);
      return h;
    }
    case 'ATXHeading2':
    case 'SetextHeading2': {
      const h = document.createElement('h2');
      attachChildren(h, node, src);
      return h;
    }
    case 'ATXHeading3': {
      const h = document.createElement('h3');
      attachChildren(h, node, src);
      return h;
    }
    case 'ATXHeading4': {
      const h = document.createElement('h4');
      attachChildren(h, node, src);
      return h;
    }
    case 'ATXHeading5': {
      const h = document.createElement('h5');
      attachChildren(h, node, src);
      return h;
    }
    case 'ATXHeading6': {
      const h = document.createElement('h6');
      attachChildren(h, node, src);
      return h;
    }

    /* ── Paragraph ────────────────────────────── */
    case 'Paragraph': {
      const p = document.createElement('p');
      attachChildren(p, node, src);
      return p;
    }

    /* ── Lists ────────────────────────────────── */
    case 'BulletList': {
      const ul = document.createElement('ul');
      attachChildren(ul, node, src);
      return ul;
    }
    case 'OrderedList': {
      const ol = document.createElement('ol');
      attachChildren(ol, node, src);
      return ol;
    }
    case 'ListItem': {
      const li = document.createElement('li');
      attachChildren(li, node, src);
      return li;
    }

    /* ── Task list items ──────────────────────── */
    case 'Task': {
      const wrap = document.createElement('div');
      wrap.className = 'gm-task';

      const cb = document.createElement('input');
      cb.type = 'checkbox';

      const marker = firstChildNamed(node, 'TaskMarker');
      if (marker) {
        const m = src.slice(marker.from, marker.to);
        cb.checked = m.trim() === '[x]';
      }
      cb.disabled = true;
      wrap.appendChild(cb);

      const textSpan = document.createElement('span');
      textSpan.className = 'gm-task-text';
      if (cb.checked) textSpan.classList.add('done');

      let pos = node.from;
      let child = node.firstChild;
      while (child) {
        if (child.type.name === 'TaskMarker') {
          pos = child.to;
          child = child.nextSibling;
          continue;
        }
        if (child.from > pos) {
          textSpan.appendChild(document.createTextNode(src.slice(pos, child.from)));
        }
        const el = renderNode(child, src);
        if (el) textSpan.appendChild(el);
        pos = child.to;
        child = child.nextSibling;
      }
      if (pos < node.to) {
        textSpan.appendChild(document.createTextNode(src.slice(pos, node.to)));
      }

      wrap.appendChild(textSpan);
      return wrap;
    }

    /* ── Blockquote ───────────────────────────── */
    case 'Blockquote': {
      const bq = document.createElement('blockquote');
      attachChildren(bq, node, src);
      return bq;
    }

    /* ── Code blocks ──────────────────────────── */
    case 'FencedCode': {
      const pre = document.createElement('pre');

      const info = firstChildNamed(node, 'CodeInfo');
      if (info) {
        const lang = document.createElement('div');
        lang.className = 'gm-lang';
        lang.textContent = src.slice(info.from, info.to);
        pre.appendChild(lang);
      }

      const code = document.createElement('code');
      const ct = firstChildNamed(node, 'CodeText');
      if (ct) {
        code.textContent = src.slice(ct.from, ct.to);
      }
      pre.appendChild(code);
      return pre;
    }
    case 'CodeBlock': {
      const pre = document.createElement('pre');
      const code = document.createElement('code');
      code.textContent = src.slice(node.from, node.to).replace(/\n$/, '');
      pre.appendChild(code);
      return pre;
    }

    /* ── Horizontal rule ───────────────────────── */
    case 'HorizontalRule': {
      return document.createElement('hr');
    }

    /* ── Inline code ──────────────────────────── */
    case 'InlineCode': {
      const code = document.createElement('code');
      code.textContent = src.slice(node.from, node.to);
      return code;
    }

    /* ── Emphasis / Strong ────────────────────── */
    case 'Emphasis': {
      const em = document.createElement('em');
      attachChildren(em, node, src);
      return em;
    }
    case 'StrongEmphasis': {
      const strong = document.createElement('strong');
      attachChildren(strong, node, src);
      return strong;
    }

    /* ── Strikethrough ────────────────────────── */
    case 'Strikethrough': {
      const del = document.createElement('del');
      attachChildren(del, node, src);
      return del;
    }

    /* ── Links ────────────────────────────────── */
    case 'Link': {
      const a = document.createElement('a');

      const urlChild = firstChildNamed(node, 'URL');
      if (urlChild) {
        a.href = src.slice(urlChild.from, urlChild.to);
      }
      a.target = '_blank';
      a.rel = 'noopener noreferrer';

      const marks = [];
      let c = node.firstChild;
      while (c) {
        if (c.type.name === 'LinkMark') marks.push(c);
        c = c.nextSibling;
      }

      const textStart = marks.length >= 2 ? marks[0].to : node.from;
      const textEnd   = marks.length >= 2 ? marks[1].from : node.to;

      let pos = textStart;
      let child = node.firstChild;
      while (child) {
        if (SKIP.has(child.type.name)) { child = child.nextSibling; continue; }
        if (child.from > pos) {
          a.appendChild(document.createTextNode(src.slice(pos, child.from)));
        }
        const el = renderNode(child, src);
        if (el) a.appendChild(el);
        pos = Math.max(pos, child.to);
        child = child.nextSibling;
      }
      if (pos < textEnd) {
        a.appendChild(document.createTextNode(src.slice(pos, textEnd)));
      }

      return a;
    }

    /* ── Autolink ─────────────────────────────── */
    case 'Autolink': {
      const a = document.createElement('a');
      const url = src.slice(node.from, node.to);
      a.href = url;
      a.target = '_blank';
      a.rel = 'noopener noreferrer';
      a.textContent = url;
      return a;
    }

    /* ── Images ───────────────────────────────── */
    case 'Image': {
      const img = document.createElement('img');

      const urlChild = firstChildNamed(node, 'URL');
      let url = '';
      if (urlChild) {
        url = src.slice(urlChild.from, urlChild.to);
        img.src = url;
      }

      const marks = [];
      let c = node.firstChild;
      while (c) {
        if (c.type.name === 'LinkMark') marks.push(c);
        c = c.nextSibling;
      }

      if (marks.length >= 2) {
        const altStart = marks[0].to;
        const altEnd   = marks[1].from;
        img.alt = src.slice(altStart, altEnd);
      }

      img.loading = 'lazy';
      return img;
    }

    /* ── Escape sequence ──────────────────────── */
    case 'Escape': {
      const raw = src.slice(node.from, node.to);
      return document.createTextNode(raw.length >= 2 ? raw[1] : raw);
    }

    /* ── Hard break ───────────────────────────── */
    case 'HardBreak': {
      return document.createElement('br');
    }

    /* ── HTML passthrough ─────────────────────── */
    case 'HTMLTag':
    case 'HTMLBlock': {
      const span = document.createElement('span');
      span.innerHTML = src.slice(node.from, node.to);
      return span;
    }

    /* ── Entity ───────────────────────────────── */
    case 'Entity': {
      const span = document.createElement('span');
      span.innerHTML = src.slice(node.from, node.to);
      return span;
    }

    /* ── Comment / ProcessingInstruction ──────── */
    case 'Comment':
    case 'CommentBlock':
    case 'ProcessingInstruction':
    case 'ProcessingInstructionBlock':
      return null;

    /* ── Tables (GFM) ─────────────────────────── */
    case 'Table': {
      const table = document.createElement('table');
      let c = node.firstChild;
      while (c) {
        if (SKIP.has(c.type.name)) { c = c.nextSibling; continue; }
        const el = renderNode(c, src);
        if (el) table.appendChild(el);
        c = c.nextSibling;
      }
      return table;
    }
    case 'TableHeader': {
      const thead = document.createElement('thead');
      const row = document.createElement('tr');
      let c = node.firstChild;
      while (c) {
        if (SKIP.has(c.type.name)) { c = c.nextSibling; continue; }
        const td = document.createElement('th');
        const txt = src.slice(c.from, c.to).trim();
        td.textContent = txt;
        row.appendChild(td);
        c = c.nextSibling;
      }
      thead.appendChild(row);
      return thead;
    }
    case 'TableRow': {
      const tr = document.createElement('tr');
      let c = node.firstChild;
      while (c) {
        if (SKIP.has(c.type.name)) { c = c.nextSibling; continue; }
        const td = document.createElement('td');
        const txt = src.slice(c.from, c.to).trim();
        td.textContent = txt;
        tr.appendChild(td);
        c = c.nextSibling;
      }
      return tr;
    }

    /* ── LinkReference (definition) — not rendered ─ */
    case 'LinkReference':
      return null;

    /* ── Emoji (GFM) — render as text ─────────── */
    case 'Emoji': {
      return document.createTextNode(src.slice(node.from, node.to));
    }

    /* ── Unknown / fallback — render as text ──── */
    default: {
      return document.createTextNode(src.slice(node.from, node.to));
    }
  }
}

async function buildContent(src) {
  const frag = document.createDocumentFragment();
  if (!src || !src.trim()) return frag;

  const mod = await import('@lezer/markdown');
  const tree = mod.parser.configure(mod.GFM).parse(src);
  const top = tree.topNode;

  let child = top.firstChild;
  while (child) {
    const el = renderNode(child, src);
    if (el) frag.appendChild(el);
    child = child.nextSibling;
  }

  return frag;
}

export default async function createWidget({ name, evalContext = {}, content = '' }) {
  ensureStyles();

  const el = document.createElement('div');
  el.className = 'gm-wrap';

  if (content) {
    const body = document.createElement('div');
    body.className = 'gm-body';
    const frag = await buildContent(content);
    body.appendChild(frag);
    el.appendChild(body);
  } else {
    const empty = document.createElement('div');
    empty.className = 'gm-empty';
    empty.textContent = '(empty)';
    el.appendChild(empty);
  }

  let controls = null;

  const decoration = {
    icon: MD_ICON,
    title: name,
  };

  return {
    el,
    decoration,

    get controls() { return controls; },
    set controls(c) { controls = c; },

    focus() { el.focus(); },
    destroy() {},
  };
}
