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
 *   icon         — 'editor'
 *   title        — filename (set after load)
 *   rightButtons — [save button]
 *   onCloseRequest — warns when dirty
 *
 * controls interface (injected by shell after mount):
 *   controls.setTitle(str)
 *   controls.setTitlebarColor(css)
 *   controls.setDirty(bool)
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
    .ge-cm .cm-editor {
      height: 100%;
      background: transparent;
      color: var(--text-hi);
    }
    .ge-cm .cm-editor.cm-focused { outline: none; }
    .ge-cm .cm-scroller {
      font-family: ${FONT_MONO};
      font-size: var(--fs-base);
      line-height: 1.6;
      overflow: auto;
    }

    .ge-cm .cm-gutters {
      background: var(--bg-elevated);
      border-right: 1px solid var(--border);
      color: var(--text-dim);
    }
    .ge-cm .cm-activeLineGutter { background: var(--bg-hover); }
    .ge-cm .cm-gutter.cm-lineNumbers .cm-gutterElement {
      padding: 0 0.75rem 0 0.5rem;
      font-size: var(--fs-sm);
    }

    .ge-cm .cm-line { padding: 0 0.75rem; }
    .ge-cm .cm-activeLine { background: var(--bg-hover); }

    .ge-cm .cm-selectionBackground { background: var(--a1-dim) !important; }
    .ge-cm.cm-focused .cm-selectionBackground { background: rgba(77,158,247,0.2) !important; }
    .ge-cm .cm-cursor { border-left-color: var(--a1); }

    .ge-cm .cm-matchingBracket {
      background: var(--a1-dim);
      color: var(--text-hi) !important;
    }

    .ge-cm .cm-searchMatch { background: var(--a1-dim); }
    .ge-cm .cm-searchMatch.cm-searchMatch-selected { background: rgba(77,158,247,0.35); }

    .ge-cm .cm-scroller::-webkit-scrollbar { width: 0.25rem; height: 0.25rem; }
    .ge-cm .cm-scroller::-webkit-scrollbar-track { background: transparent; }
    .ge-cm .cm-scroller::-webkit-scrollbar-thumb { background: var(--border-hi); border-radius: 0.125rem; }
  `);
}

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
// name : display name passed by the shell
// path : string file path (used for language detection)
// r    : { serverUrl, volume, path } — resolved path object
// fs   : GritsClient instance
export default function createWidget({ name, path = null, r = null, fs }) {
  ensureStyles();

  const el = document.createElement('div');
  el.className = 'ge-wrap';

  let view  = null;
  let dirty = false;

  // controls is injected by the shell after mount
  let controls = null;

  // ── save button descriptor ────────────────────────────
  // Kept as an object so we can mutate .enabled and the shell
  // re-reads it on the next render cycle, but we also update
  // the live DOM element directly via _el for instant feedback.
  const saveBtn = {
    icon:    'save',
    label:   'save  (⌘S)',
    enabled: false,
    action() { save(); },
  };

  // ── decoration declaration ────────────────────────────
  // The shell reads this once after the factory resolves,
  // then keeps controls in sync via the controls object.
  const decoration = {
    icon: 'editor',

    // title is left as undefined here; the shell uses the
    // name passed to the factory until we call controls.setTitle()

    leftButtons:  [saveBtn],

    // Called by the shell before closing — return false to cancel
    onCloseRequest() {
      if (!dirty) return true;
      return confirm(`"${name}" has unsaved changes. Close anyway?`);
    },
  };

  // ── helpers ───────────────────────────────────────────
  function markDirty(isDirty) {
    if (dirty === isDirty) return;
    dirty = isDirty;

    // Update the save button's appearance immediately via its live DOM element
    if (saveBtn._el) {
      saveBtn._el.style.opacity = isDirty ? '1' : '0.3';
      saveBtn._el.disabled = !isDirty;
    }
    saveBtn.enabled = isDirty;

    // Push title color + save button state through the controls interface
    controls?.setDirty(isDirty);
  }

  function vol() {
    if (!r || !fs) throw new Error('codemirror: no resolved path or fs');
    return fs.volume(r.serverUrl, r.volume);
  }

  // ── load ──────────────────────────────────────────────
  async function load() {
    if (!r) { mountCM(''); return; }
    try {
      const file = await vol().lookup(r.path);
      const text = await file.text();
      mountCM(text);
    } catch (e) {
      const errEl = document.createElement('div');
      errEl.className = 'ge-error';
      errEl.textContent = `Error loading ${path}: ${e.message}`;
      el.appendChild(errEl);
    }
  }

  // ── save ──────────────────────────────────────────────
  async function save() {
    if (!r || !view) return;
    try {
      const v      = vol();
      const text   = view.state.doc.toString();
      const bytes  = new TextEncoder().encode(text);
      const contentCID = await v.put(bytes);
      const metaCID    = await v.mkfile(contentCID, bytes.byteLength);
      await v.link(metaCID, r.path);
      markDirty(false);
    } catch (e) {
      console.error('[codemirror] save failed:', e);
    }
  }

  // ── mount ─────────────────────────────────────────────
  async function mountCM(initialText) {
    const lang = await loadLang(path);

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
      EditorView.theme({ '&': { height: '100%' } }),
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