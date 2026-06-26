import { GimbalResult } from '../gimbal/result.js';
import { GimbalPath } from '../gimbal/path.js';
import { GimbalShell } from '../gimbal/gsh.js';
import { WIDGET_ICONS } from '../style/icons.js';

export const help = `\
gterm — open terminal widget

Usage:
  gsh.gterm()          open a terminal
  gsh.gterm('cmd')     open terminal and run a command`;

export function invoke(prev, ...cmdArgs) {
  if (!(prev instanceof GimbalShell)) throw new Error('gterm: must be called on gsh');

  const shell = prev;
  return new GimbalResult(async () => {
    const mod = await import('./gwm-widget.js');
    await window.gimbal.openWidget(mod, {
      name: 'terminal',
      icon: WIDGET_ICONS.gterm.icon,
      iconColor: WIDGET_ICONS.gterm.iconColor,
      zone: 'master',
      shell,
      args: cmdArgs,
    });
  });
}
