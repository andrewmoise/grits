import { GimbalResult } from '../gimbal/result.js';
import { GimbalPath } from '../gimbal/path.js';
import { GimbalShell } from '../gimbal/gsh.js';
import { WIDGET_ICONS } from '../style/icons.js';

export const help = `\
files — open file browser widget

Usage:
  gsh.files(path)       open file browser at path
  gsh.files()           open file browser in cwd`;

function resolvePath(prev, args) {
  if (prev instanceof GimbalPath) return prev;
  if (prev instanceof GimbalShell) {
    const p = args.find(a => a instanceof GimbalPath);
    if (p) return p;
    const str = args.find(a => typeof a === 'string');
    if (str) return new GimbalPath('/' + prev.resolvePath(str).path, prev);
  }
  return null;
}

export function invoke(prev, ...args) {
  if (!(prev instanceof GimbalShell)) throw new Error('files: must be called on gsh');

  const shell = prev;
  return new GimbalResult(async () => {
    const mod = await import('./gwm-widget.js');
    await window.gimbal.openWidget(mod, {
      name: 'files',
      icon: WIDGET_ICONS.files.icon,
      iconColor: WIDGET_ICONS.files.iconColor,
      zone: 'master',
      shell,
      args,
    });
  });
}
