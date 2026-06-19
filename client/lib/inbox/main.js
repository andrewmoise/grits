import { VOID } from '../gimbal/gsh.js';

export const help = `inbox — open inbox widget`;

export async function invoke(shell, previous, args) {
  const mod = await import('./gwm-widget.js');
  await window.gimbal.openWidget(mod, {
    name: 'inbox',
    icon: 'inbox',
    zone: 'master',
    evalContext: { fs: shell.fs, shell },
    args,
  });
  return VOID;
}
