import { VOID } from '../gimbal/gsh.js';

export const help = `inbox — open inbox widget`;

export async function invoke(shell, previous, args) {
  const mod = await import('./gwm-widget.js');
  await window.gimbal.openWidget(mod, {
    name: '',
    icon: 'inbox',
    zone: 'master',
    shell,
    args,
  });
  return VOID;
}
