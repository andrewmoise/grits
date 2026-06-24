import { VOID } from '../gimbal/gsh.js';

export const help = `editor [path] — open a file in the editor`;

export async function invoke(shell, previous, args) {
  const arg = args[0];

  let r    = null;

  if (typeof arg === 'string') {
    r    = shell.resolvePath(arg);
  } else if (arg && typeof arg === 'object') {
    r    = arg;
  }

  const mod = await import('./gwm-widget.js');
  await window.gimbal.openWidget(mod, { name: '', icon: 'editor', zone: 'master', shell, file: r });

  return VOID;
}
