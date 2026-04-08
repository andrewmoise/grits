import { VOID } from '../gimbal/gsh.js';

export const help = `editor [path] — open a file in the editor`;

export async function invoke(shell, previous, args) {
  if (!shell.ui) throw new Error('editor: no window manager available');

  const path = typeof args[0] === 'string' ? args[0] : null;
  const name = path ? path.split('/').pop() : 'editor';
  const r    = path ? shell.resolvePath(path) : null;

  const { default: createWidget } = await import('../codemirror/gwm-widget.js');
  const instance = createWidget({ name, path, r, fs: shell.fs });

  shell.ui.openWidget({ ...instance, name, icon: 'editor', zone: 'stack' });
  return VOID;
}