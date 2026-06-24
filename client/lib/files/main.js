import { VOID } from '../gimbal/gsh.js';

export const help = `files [path] — open file browser widget`;

export async function invoke(shell, previous, args) {
  const mod = await import('./gwm-widget.js');

  const path = typeof args[0] === 'string'
    ? args[0]
    : (args[0] && typeof args[0] === 'object' ? args[0].path : null);

  const name = path ? path.split('/').filter(Boolean).pop() || '/' : 'files';

  let r = null;
  if (path) {
    try { r = shell.resolvePath(path); } catch {}
  }

  await window.gimbal.openWidget(mod, {
    name,
    icon: 'files',
    zone: 'master',
    shell,
    args,
  });

  return VOID;
}
