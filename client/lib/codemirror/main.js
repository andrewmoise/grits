import { GimbalResult } from '../gimbal/result.js';
import { GimbalPath } from '../gimbal/path.js';
import { GimbalShell } from '../gimbal/gsh.js';
import { WIDGET_ICONS } from '../style/icons.js';

export const help = `\
codemirror — open a file in the editor

Usage:
  path.codemirror()          open file at path in editor
  gsh.codemirror(path)       same (path must be GimbalPath)`;

function resolvePath(prev, args) {
  if (prev instanceof GimbalPath) return prev;
  if (prev instanceof GimbalShell) {
    return args.find(a => a instanceof GimbalPath) || null;
  }
  return null;
}

export function invoke(prev, ...args) {
  const path = resolvePath(prev, args);
  if (!(path instanceof GimbalPath)) throw new Error('codemirror: need a file path');

  const shell = path._shell;
  return new GimbalResult(async () => {
    const r = shell.resolvePath(path.abs());
    const mod = await import('./gwm-widget.js');
    await window.gimbal.openWidget(mod, {
      name: '',
      icon: WIDGET_ICONS.edit.icon,
      iconColor: WIDGET_ICONS.edit.iconColor,
      zone: 'master',
      shell,
      file: r,
    });
  });
}
