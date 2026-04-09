import { VOID } from '../gimbal/gsh.js';

export const help = `editor [path] — open a file in the editor`;

export async function invoke(shell, previous, args) {
  const path = typeof args[0] === 'string' ? args[0] : null;
  const name = path ? path.split('/').pop() : 'editor';
  const r    = path ? shell.resolvePath(path) : null;

  const mod = await import('../codemirror/gwm-widget.js');
  await window.gimbal.openWidget(mod, { name, icon: 'editor', zone: 'master', path, r, fs: shell.fs });

  return VOID;
}