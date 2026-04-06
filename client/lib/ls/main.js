// lib/ls/main.js
export const help = `\
ls — list directory contents

Usage:
  ls()                list current directory (void input, no arg)
  ls('some/path')     list a path (void input, path arg)
  <dir>.ls()          list a directory passed in as input (no arg)

Options:
  {raw:true}   return raw { name: metadataCID } map
  {l:true}     return { name: metadata } map (long listing)`;

import { isVoid, _isPlainObject } from '../gimbal/gsh.js';
import { GritsFile } from '../grits/GritsClient.js';

export async function invoke(shell, previous, args) {
  const opts       = _isPlainObject(args[args.length - 1]) ? args[args.length - 1] : {};
  const positional = opts === args[args.length - 1] ? args.slice(0, -1) : [...args];
  const [pathArg]  = positional;

  const prev = await previous;

  let file;
  if (!isVoid(prev)) {
    // Pipeline mode — no path arg allowed
    if (pathArg !== undefined)
      throw new Error('ls: cannot combine pipeline input with path argument');
    if (!(prev instanceof GritsFile))
      throw new Error('ls: pipeline input must be a GritsFile');
    file = prev;
  } else {
    // Argument mode — optional path, falls back to cwd
    if (pathArg !== undefined) {
      if (typeof pathArg !== 'string')
        throw new Error('ls: path argument must be a string');
      const { serverUrl, volume, path } = shell.resolvePath(pathArg);
      file = await shell._vol(serverUrl, volume).lookup(path);
    } else {
      // No path arg — use current location directly.
      const { serverUrl, volume, path } = shell.resolvePath('.');
      file = await shell._vol(serverUrl, volume).lookup(path);
    }
  }

  if (!file.isDir())
    throw new Error(`ls: not a directory`);

  const dirMap = await file.json();

  if (opts.raw)  return dirMap;

  if (opts.l || opts.long) {
    const vol     = shell._currentVol();
    const entries = {};
    await Promise.all(
      Object.entries(dirMap).map(async ([name, metaCID]) => {
        entries[name] = await vol.meta(metaCID);
      })
    );
    return entries;
  }

  return Object.keys(dirMap).sort();
}