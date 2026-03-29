import { VOID } from '../gimbal/gsh.js';
export const help = `files — open the file browser`;
export async function invoke(shell, previous, args) {
  if (!shell.ui) throw new Error('files: no window manager available');
  const { default: createWidget } = await import('./gwm-widget.js');
  const instance = createWidget({ name: 'File Browser', evalContext: shell._evalContext });
  shell.ui.openWidget({ ...instance, icon: 'files', zone: 'master' });
  return VOID;
}