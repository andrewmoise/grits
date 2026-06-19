import { VOID, isVoid } from '../gimbal/gsh.js';

export const help = `\
markdown — render Markdown content

Usage:
  markdown('path/to/file.md')   render a .md file from GritsFS
  <input> | markdown()          render piped text/Response as Markdown

  Only string file paths or piped text input are accepted.`;

export async function invoke(shell, previous, args) {
  const prev = await previous;
  let content = null;
  let title = 'markdown';

  if (args.length > 0 && typeof args[0] === 'string') {
    const path = args[0];
    const parts = path.split('/').filter(Boolean);
    title = parts.pop() || 'markdown';

    if (!isVoid(prev)) {
      console.warn('markdown: ignoring piped input when file path given');
    }

    const { serverUrl, volume, path: relPath } = shell.resolvePath(path);
    const file = await shell._vol(serverUrl, volume).lookup(relPath);
    const resp = await file.get();
    content = await resp.text();

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
    icon: 'markdown',
    zone: 'master',
    evalContext: { fs: shell.fs, shell },
    content,
  });

  return VOID;
}
