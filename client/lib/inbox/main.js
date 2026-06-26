import { GimbalResult } from '../gimbal/result.js';
import { GimbalShell } from '../gimbal/gsh.js';
import { WIDGET_ICONS } from '../style/icons.js';

export const help = `\
inbox — open inbox widget

Usage:
  gsh.inbox()          open inbox`;

export function invoke(prev, ...args) {
  if (!(prev instanceof GimbalShell)) throw new Error('inbox: must be called on gsh');

  const shell = prev;
  return new GimbalResult(async () => {
    const mod = await import('./gwm-widget.js');
    await window.gimbal.openWidget(mod, {
      name: '',
      icon: WIDGET_ICONS.inbox.icon,
      iconColor: WIDGET_ICONS.inbox.iconColor,
      zone: 'master',
      shell,
      args,
    });
  });
}
