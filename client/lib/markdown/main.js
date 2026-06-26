import { GimbalResult } from '../gimbal/result.js';
import { GimbalPath } from '../gimbal/path.js';
import { GimbalShell } from '../gimbal/gsh.js';
import { WIDGET_ICONS } from '../style/icons.js';

export const help = `\
markdown — render Markdown content

Usage:
  path.markdown()              render file at path as Markdown
  gsh.markdown(path)           same (path must be GimbalPath)`;

function resolvePath(prev, args) {
  if (prev instanceof GimbalPath) return prev;
  if (prev instanceof GimbalShell) {
    return args.find(a => a instanceof GimbalPath) || null;
  }
  return null;
}

export function invoke(prev, ...args) {
  const path = resolvePath(prev, args);
  if (!(path instanceof GimbalPath)) throw new Error('markdown: need a file path');

  const shell = path._shell;
  return new GimbalResult(() => _render(shell, path));
}

async function _render(shell, path) {
  const parts = path.abs().split('/').filter(Boolean);
  const title = parts.pop() || 'markdown';

  const r = shell.resolvePath(path.abs());
  const file = await shell._vol(r.serverUrl, r.volume).lookup(r.path);
  const resp = await file.get();
  const content = await resp.text();

  const dirParts = r.path.split('/').filter(Boolean);
  dirParts.pop();
  const encPath = dirParts.map(encodeURIComponent).join('/');
  const sourceDir = encPath
    ? `${r.serverUrl}/grits/v1/content/${r.volume}/${encPath}`
    : `${r.serverUrl}/grits/v1/content/${r.volume}`;

  const mod = await import('./gwm-widget.js');
  await window.gimbal.openWidget(mod, {
    name: title,
    icon: WIDGET_ICONS.markdown.icon,
    iconColor: WIDGET_ICONS.markdown.iconColor,
    zone: 'master',
    shell,
    content,
    sourceDir,
  });
}
