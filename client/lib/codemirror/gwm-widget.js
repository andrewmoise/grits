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
 */

import { C, alpha, FONT_MONO, injectStyles } from '../style/style.js';
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
export default function createWidget({ name, file = null, shell }) {
  if (typeof file === 'string' && shell) {
    file = shell.resolvePath(file);
  }

  ensureStyles();

  const el = document.createElement('div');
  el.className = 'ge-wrap';

  let view      = null;
  let dirty     = false;
  let currentPath = file?.path ?? null;
  let currentName = file ? file.path.split('/').pop() : name;
  let currentR    = file;

  const fs    = shell?.fs;

  // controls is injected by the shell after mount
  let controls = null;

  // ── save button descriptor ────────────────────────────
  const saveBtn = {
    icon:    'save',
    label:   'save  (⌘S)',
    enabled: !!file,
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

  function volFor(rr) {
    if (!rr || !fs) throw new Error('codemirror: no resolved path or fs');
    return fs.volume(rr.serverUrl, rr.volume);
  }

  // ── load ──────────────────────────────────────────────
  async function load() {
    if (!currentR) { mountCM(''); return; }
    try {
      const file = await volFor(currentR).lookup(currentR.path);
      const text = await file.text();
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
      const v      = volFor(currentR);
      const text   = view.state.doc.toString();
      const bytes  = new TextEncoder().encode(text);
      const contentCID = await v.put(bytes);
      const metaCID    = await v.mkfile(contentCID, bytes.byteLength);
      await v.link(metaCID, currentR.path);
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

    const defaultName = currentR
      ? (currentR.volume === 'primary' ? `/${currentR.path}` : `//${currentR.volume}/${currentR.path}`)
      : shell
        ? (shell.volume === 'primary' ? `/${shell.cwd || ''}` : `//${shell.volume}/${shell.cwd || ''}`)
        : '(untitled)';
    const answer = await promptInput({ message: 'Save as…', defaultValue: defaultName });
    if (!answer) return;

    let resolved;
    if (shell) {
      resolved = shell.resolvePath(answer);
    } else if (currentR) {
      // Fall back to current volume if no shell
      resolved = { ...currentR, path: answer };
    } else {
      toast('Cannot save: no shell context to resolve path');
      return;
    }

    try {
      const v      = volFor(resolved);
      const text   = view.state.doc.toString();
      const bytes  = new TextEncoder().encode(text);
      const contentCID = await v.put(bytes);
      const metaCID    = await v.mkfile(contentCID, bytes.byteLength);
      await v.link(metaCID, resolved.path);

      currentR    = resolved;
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
      EditorView.theme({
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
          backgroundColor: alpha(C.blue, 0.35),
        },
        '.cm-selectionBackground': { backgroundColor: C.blueDim },
        '&.cm-focused .cm-selectionBackground': {
          backgroundColor: alpha(C.blue, 0.25),
        },
        '.cm-line::selection, .cm-line ::selection': {
          backgroundColor: 'transparent',
        },
        '&.cm-focused .cm-line::selection, &.cm-focused .cm-line ::selection': {
          backgroundColor: alpha(C.blue, 0.25),
        },
      }),
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