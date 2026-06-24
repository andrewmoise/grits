import { VOID, isVoid, _isPlainObject } from '../gimbal/gsh.js';
import { WIDGET_ICONS } from '../style/icons.js';

export const help = `\
markdown — render Markdown content

Usage:
  markdown('path/to/file.md')   render a .md file from GritsFS
  <input> | markdown()          render piped text/Response as Markdown
  markdown('path', { iconColor: 'teal-hi' })  override icon color

  Only string file paths or piped text input are accepted.`;

export async function invoke(shell, previous, args) {
  const prev = await previous;
  const opts       = _isPlainObject(args[args.length - 1]) ? args[args.length - 1] : {};
  const positional = opts === args[args.length - 1] ? args.slice(0, -1) : [...args];
  const defaults   = WIDGET_ICONS.markdown;

  let content = null;
  let title = 'markdown';
  let sourceDir = '';

  if (positional.length > 0 && typeof positional[0] === 'string') {
    const path = positional[0];
    const parts = path.split('/').filter(Boolean);
    title = parts.pop() || 'markdown';

    if (!isVoid(prev)) {
      console.warn('markdown: ignoring piped input when file path given');
    }

    const { serverUrl, volume, path: relPath } = shell.resolvePath(path);
    const file = await shell._vol(serverUrl, volume).lookup(relPath);
    const resp = await file.get();
    content = await resp.text();

    const dirParts = relPath.split('/').filter(Boolean);
    dirParts.pop();
    const encPath = dirParts.map(encodeURIComponent).join('/');
    sourceDir = encPath
      ? `${serverUrl}/grits/v1/content/${volume}/${encPath}`
      : `${serverUrl}/grits/v1/content/${volume}`;

  } else if (!isVoid(prev)) {
    if (prev instanceof Response) {
      content = await prev.text();
    } else if (typeof prev === 'string' || prev instanceof String) {
      content = String(prev);
    } else {
      const t = prev?.constructor?.name ?? typeof prev;
      throw new Error(`markdown: piped input must be text, got ${t}`);
    }

  } else {
    throw new Error(`markdown: a file path or piped input is required`);
  }

  const mod = await import('./gwm-widget.js');
  await window.gimbal.openWidget(mod, {
    name: title,
    icon:      opts.icon      ?? defaults.icon,
    iconColor: opts.iconColor ?? defaults.iconColor,
    zone: 'master',
    shell,
    content,
    sourceDir,
  });

  return VOID;
}
