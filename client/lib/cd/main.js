import { VOID, isVoid } from '../gimbal/gsh.js';

export const help = `\
cd — change current directory

Usage:
  cd('path')              relative or absolute path
  cd(':volume/path')      different volume, same server
  cd('host:vol/path')     different server and volume`;

export async function invoke(shell, previous, args) {
  const prev = await previous;
  if (!isVoid(prev))
    throw new Error('cd: does not accept pipeline input');

  const [path = '/'] = args;
  if (typeof path !== 'string')
    throw new Error('cd: path must be a string');

  // Path parsing and state mutation — same logic as _builtinCd
  let serverUrl = shell.serverUrl;
  let volume    = shell.volume;
  let cwd       = shell.cwd || '/';
  let rest      = path;

  const slashIdx = rest.indexOf('/');
  const colonIdx = rest.indexOf(':');
  if (colonIdx !== -1 && (slashIdx === -1 || colonIdx < slashIdx)) {
    const hostPart  = rest.slice(0, colonIdx);
    rest            = rest.slice(colonIdx + 1);
    const nextSlash = rest.indexOf('/');
    const volPart   = nextSlash === -1 ? rest : rest.slice(0, nextSlash);
    rest            = nextSlash === -1 ? ''   : rest.slice(nextSlash + 1);
    if (hostPart) serverUrl = hostPart.startsWith('http') ? hostPart : `https://${hostPart}`;
    volume = volPart || volume;
    cwd    = '/';
  }

  if (rest.startsWith('/')) {
    cwd  = '/';
    rest = rest.replace(/^\/+/, '');
  }

  if (rest) {
    cwd = (cwd === '/' ? '/' : cwd + '/') + rest;
    cwd = cwd.replace(/\/+/g, '/');
  }

  const vol  = shell.gg.volume(serverUrl, volume);
  const file = await vol.lo(cwd.replace(/^\//, ''));
  if (!file.isDir()) throw new Error(`cd: not a directory: ${path}`);

  shell.serverUrl = serverUrl;
  shell.volume    = volume;
  shell.cwd       = cwd;
  return VOID;
}