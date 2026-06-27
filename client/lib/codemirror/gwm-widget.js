/*
 * @cell codemirror-widget
 * @version 0.4
 * @about
 *   CodeMirror 6 editor widget for the Gimbal shell.
 *   Reads files via r (resolved path) + fs.volume().lookup().
 *   Saves via vol.put() + vol.mkfile() + vol.link().
 * @implements gimbal-shell#widget
 *
 * decoration interface:
 *   icon         — 'edit'
 *   title        — filename (set after load)
 *   rightButtons — [save button]
 *   onCloseRequest — warns when dirty
 *
 * controls interface (injected by shell after mount):
 *   controls.setTitle(str)
 *   controls.setTitlebarColor(css)
 *   controls.setDirty(bool)
 *
 * theming:
 *   Editor chrome is themed off CSS custom properties so it tracks the host
 *   app. Selection colors honor optional overrides and otherwise derive live
 *   from the --a1 accent via color-mix:
 *     --cm-selection          unfocused selection background
 *     --cm-selection-focused  focused selection background
 *     --cm-selection-search   selected search match background
 */

import { FONT_MONO, injectStyles } from '../style/style.js';
import { EditorView, keymap, lineNumbers, highlightActiveLine,
         highlightActiveLineGutter, drawSelection, dropCursor } from '@codemirror/view';
import { EditorState } from '@codemirror/state';
import { defaultKeymap, history, historyKeymap, indentWithTab } from '@codemirror/commands';
import { bracketMatching } from '@codemirror/language';
import { searchKeymap, highlightSelectionMatches } from '@codemirror/search';
import { closeBrackets, closeBracketsKeymap } from '@codemirror/autocomplete';
import { syntaxTheme } from './theme.js';
import { toast, promptInput, confirmDialog } from '../gimbal/dialog.js';

const STYLE_ID = 'gimbal-codemirror-styles';

function ensureStyles() {
  injectStyles(STYLE_ID, `
    .ge-wrap {
      width: 100%; height: 100%;
      display: flex; flex-direction: column;
      overflow: hidden;
    }

    .ge-error {
      padding: 0.75rem;
      color: var(--red);
      font-family: ${FONT_MONO};
      font-size: var(--fs-base);
    }

    .ge-cm {
      flex: 1;
      overflow: hidden;
    }
  `);
}

// ── Editor theme ─────────────────────────────────────────
// Static — references only module-level imports and CSS vars, so it's built
// once at module load instead of per-mount. EditorView.theme() auto-scopes to
// this widget's instances, so nothing here leaks to other CodeMirror editors.
// Flip { dark: true } if the host app is a light theme.
const appTheme = EditorView.theme({
  '&': {
    height: '100%',
    background: 'transparent',
    color: 'var(--text-hi)',
  },
  '&.cm-focused': { outline: 'none' },
  '.cm-scroller': {
    fontFamily: FONT_MONO,
    fontSize: 'var(--fs-base)',
    lineHeight: 1.6,
    overflow: 'auto',
  },
  '.cm-scroller::-webkit-scrollbar': { width: '0.25rem', height: '0.25rem' },
  '.cm-scroller::-webkit-scrollbar-track': { background: 'transparent' },
  '.cm-scroller::-webkit-scrollbar-thumb': {
    background: 'var(--border-hi)',
    borderRadius: '0.125rem',
  },
  '.cm-gutters': {
    background: 'var(--bg-elevated)',
    borderRight: '1px solid var(--border)',
    color: 'var(--text-dim)',
  },
  '.cm-activeLineGutter': { backgroundColor: 'var(--bg-hover)' },
  '.cm-gutter.cm-lineNumbers .cm-gutterElement': {
    padding: '0 0.75rem 0 0.5rem',
    fontSize: 'var(--fs-sm)',
  },
  '.cm-line': { padding: '0 0.75rem' },
  '.cm-activeLine': { backgroundColor: 'var(--bg-hover)' },
  '.cm-cursor': { borderLeftColor: 'var(--a1)' },
  '.cm-matchingBracket': {
    backgroundColor: 'var(--a1-dim)',
    color: 'var(--text-hi)',
  },
  '.cm-searchMatch': { backgroundColor: 'var(--a1-dim)' },
  '.cm-searchMatch.cm-searchMatch-selected': {
    backgroundColor: 'var(--cm-selection-search, color-mix(in srgb, var(--a1) 35%, transparent))',
  },
  '.cm-selectionBackground': {
    backgroundColor: 'var(--cm-selection, color-mix(in srgb, var(--a1) 18%, transparent))',
  },
  '&.cm-focused .cm-selectionBackground': {
    backgroundColor: 'var(--cm-selection-focused, color-mix(in srgb, var(--a1) 25%, transparent))',
  },
  '.cm-line::selection, .cm-line ::selection': {
    backgroundColor: 'transparent',
  },
  '&.cm-focused .cm-line::selection, &.cm-focused .cm-line ::selection': {
    backgroundColor: 'var(--cm-selection-focused, color-mix(in srgb, var(--a1) 25%, transparent))',
  },
}, { dark: true });

// ── Language pack loader ─────────────────────────────────
async function loadLang(path) {
  const ext = path?.split('.').pop()?.toLowerCase();
  const langMap = {
    js: 'lang-javascript', mjs: 'lang-javascript', ts: 'lang-javascript',
    css: 'lang-css', html: 'lang-html', json: 'lang-json',
    md: 'lang-markdown', py: 'lang-python',
  };
  const langPkg = langMap[ext];
  if (!langPkg) return null;
  try {
    const mod = await import(`/lib/node_modules/@codemirror/${langPkg}/dist/index.js`);
    const fn  = mod.javascript ?? mod.css ?? mod.html ?? mod.json
              ?? mod.markdown  ?? mod.python ?? Object.values(mod)[0];
    return typeof fn === 'function' ? fn() : null;
  } catch (e) {
    console.warn(`[codemirror] language pack ${langPkg} unavailable:`, e.message);
    return null;
  }
}

// ── Widget factory ────────────────────────────────────────
export default function createWidget({ name, path: gimbalPath = null, gimbal }) {
  ensureStyles();

  const el = document.createElement('div');
  el.className = 'ge-wrap';

  let view      = null;
  let dirty     = false;
  let currentPath = gimbalPath?.abs() ?? null;
  let currentName = gimbalPath?.toString() ?? name;
  let currentR    = gimbalPath;

  // controls is injected by the shell after mount
  let controls = null;

  // ── save button descriptor ────────────────────────────
  const saveBtn = {
    icon:    'save',
    label:   'save  (⌘S)',
    enabled: !!gimbalPath,
    action() { save(); },
  };

  const saveAsBtn = {
    icon:    'saveAs',
    label:   'save as…',
    enabled: true,
    action() { saveAs(); },
  };

  const newBtn = {
    icon:    'newDoc',
    label:   'new document',
    enabled: true,
    action() { newDocument(); },
  };

  // ── decoration declaration ────────────────────────────
  const decoration = {
    icon: 'edit',
    title: currentName,
    leftButtons: [saveBtn, saveAsBtn, newBtn],

    async onCloseRequest() {
      if (!dirty) return true;
      const displayName = currentName || 'Untitled';
      return confirmDialog({ message: `"${displayName}" has unsaved changes. Close anyway?` });
    },
  };

  // ── helpers ───────────────────────────────────────────
  function markDirty(isDirty) {
    if (dirty === isDirty) return;
    dirty = isDirty;

    const hasPath = !!currentR;

    if (saveBtn._el) {
      saveBtn._el.style.color  = (isDirty && hasPath) ? 'var(--a2)' : '';
      saveBtn._el.disabled     = !hasPath;
      saveBtn._el.style.opacity = hasPath ? '1' : '0.3';
    }

    if (saveAsBtn._el) {
      saveAsBtn._el.style.color = (isDirty && !hasPath) ? 'var(--a2)' : '';
    }

    controls?.setDirty(isDirty);
  }

  function _resolve(rr) {
    if (!rr || !gimbal) throw new Error('codemirror: no file path or client');
    return gimbal.resolvePath(rr.abs());
  }

  // ── load ──────────────────────────────────────────────
  async function load() {
    if (!currentR) { mountCM(''); return; }
    try {
      console.log('[codemirror] gimbalPath.abs():', currentR.abs());
      const r = _resolve(currentR);
      console.log('[codemirror] resolvePath result:', r);
      console.log('[codemirror] volume:', r.volumeName, 'path:', r.path, 'serverUrl:', gimbal._serverUrl);
      const v = gimbal.grits.volume(gimbal._serverUrl, r.volumeName);
      const gritsFile = await v.lookup(r.path);
      const text = await gritsFile.text();
      mountCM(text);
    } catch (e) {
      const errEl = document.createElement('div');
      errEl.className = 'ge-error';
      errEl.textContent = `Error loading ${currentPath}: ${e.message}`;
      el.appendChild(errEl);
    }
  }

  // ── save ──────────────────────────────────────────────
  async function save() {
    if (!currentR || !view) return;
    try {
      const r    = _resolve(currentR);
      const v    = gimbal.grits.volume(gimbal._serverUrl, r.volumeName);
      const text = view.state.doc.toString();
      const bytes = new TextEncoder().encode(text);
      const contentCID = await v.put(bytes);
      const metaCID    = await v.mkfile(contentCID, bytes.byteLength);
      await v.link(metaCID, r.path);
      markDirty(false);
    } catch (e) {
      toast(`Save failed: ${e.message}`);
    }
  }

  // ── new document ─────────────────────────────────────
  async function newDocument() {
    const displayName = currentName || 'Untitled';
    if (dirty && !(await confirmDialog({ message: `"${displayName}" has unsaved changes. Discard?` }))) return;

    currentName = '';
    currentPath = null;
    currentR    = null;

    if (view) {
      view.dispatch({ changes: { from: 0, to: view.state.doc.length, insert: '' } });
      markDirty(false);
    } else {
      mountCM('');
    }

    controls?.setTitle(currentName);
  }

  // ── save as ───────────────────────────────────────────
  async function saveAs() {
    if (!view) return;

    const defaultName = currentR ? currentR.abs() : '(untitled)';
    const answer = await promptInput({ message: 'Save as…', defaultValue: defaultName });
    if (!answer) return;

    try {
      const newR  = gimbal.p(answer);
      const r     = _resolve(newR);
      const v     = gimbal.grits.volume(gimbal._serverUrl, r.volumeName);
      const text  = view.state.doc.toString();
      const bytes = new TextEncoder().encode(text);
      const contentCID = await v.put(bytes);
      const metaCID    = await v.mkfile(contentCID, bytes.byteLength);
      await v.link(metaCID, r.path);

      currentR    = newR;
      currentPath = answer;
      currentName = answer.split('/').pop();
      controls?.setTitle(currentName);
      markDirty(false);
    } catch (e) {
      toast(`Save failed: ${e.message}`);
    }
  }

  // ── mount ─────────────────────────────────────────────
  async function mountCM(initialText) {
    const lang = await loadLang(currentPath);

    const extensions = [
      lineNumbers(),
      highlightActiveLineGutter(),
      highlightActiveLine(),
      drawSelection(),
      dropCursor(),
      bracketMatching(),
      closeBrackets(),
      history(),
      syntaxTheme,
      highlightSelectionMatches(),
      keymap.of([
        ...closeBracketsKeymap,
        ...defaultKeymap,
        ...searchKeymap,
        ...historyKeymap,
        indentWithTab,
        { key: 'Mod-s', run() { save(); return true; }, preventDefault: true },
      ]),
      EditorView.updateListener.of(update => {
        if (update.docChanged) markDirty(true);
      }),
      appTheme,
    ];

    if (lang) extensions.push(lang);

    const host = document.createElement('div');
    host.className = 'ge-cm';
    el.appendChild(host);

    view = new EditorView({
      state: EditorState.create({ doc: initialText, extensions }),
      parent: host,
    });

    markDirty(false);
  }

  load();

  return {
    el,
    decoration,

    // The shell writes to this after mount
    get controls() { return controls; },
    set controls(c) { controls = c; },

    focus()   { view?.focus(); },
    destroy() { view?.destroy(); },
    save,
  };
}