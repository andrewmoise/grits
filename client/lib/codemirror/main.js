import { VOID } from '../gimbal/gsh.js';

export const help = `editor [path] — open a file in the editor`;

export async function invoke(shell, previous, args) {
  const arg = args[0];

  let path = null;
  let r    = null;

  if (typeof arg === 'string') {
    path = arg;
    r    = shell.resolvePath(path);
  } else if (arg && typeof arg === 'object') {
    r    = arg;
    path = arg.path ?? null;
  }

  const name = path ? path.split('/').pop() : 'editor';

  const mod = await import('../codemirror/gwm-widget.js');
  await window.gimbal.openWidget(mod, { name, icon: 'editor', zone: 'master', path, r, fs: shell.fs });

  return VOID;
}
