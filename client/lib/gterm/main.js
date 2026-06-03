import { VOID } from '../gimbal/gsh.js';

export const help = `gterm — open terminal widget`;

export async function invoke(shell, previous, args) {
  const mod = await import('./gwm-widget.js');

  await window.gimbal.openWidget(mod, {
    name: 'terminal',
    icon: 'terminal',
    zone: 'master',
    evalContext: {
      fs: shell.fs,
      shell,
    },
    args,
  });

  return VOID;
}
