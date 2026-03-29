import { VOID } from '../gimbal/gsh.js';
export const help = `gterm — open a new terminal window`;
export async function invoke(shell, previous, args) {
  if (!shell.ui) throw new Error('gterm: no window manager available');
  const { default: createWidget } = await import('./gwm-widget.js');
  const instance = createWidget({ name: 'Terminal', evalContext: shell._evalContext });
  shell.ui.openWidget(instance);
  return VOID;
}