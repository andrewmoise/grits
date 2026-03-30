// lib/ls/main.js — list directory contents

export const help = `\
ls — list directory contents

Usage:
  ls()                list current directory
  ls('some/path')     list a relative or absolute path
  ls({raw:true})      return raw { name: metadataCID } map
  ls({l:true})        return { name: metadata } map (long listing)
  <dir>.ls()          list a directory passed in as input

Input:  GritsFile, CID, path string, or void (uses cwd)
Output: string[] of names, or object if raw/l option given`;

import { coerceToFile, isVoid, _isPlainObject } from '../gimbal/gsh.js';

export async function invoke(shell, previous, args) {
  const opts       = _isPlainObject(args[args.length - 1]) ? args[args.length - 1] : {};
  const positional = opts === args[args.length - 1] ? args.slice(0, -1) : [...args];
  const [pathArg]  = positional;

  // Resolve which directory to list
  let file;
  if (pathArg !== undefined) {
    file = await coerceToFile(pathArg, shell);
  } else {
    const prev = await previous;
    file = isVoid(prev)
      ? await coerceToFile(shell.cwd, shell)
      : await coerceToFile(prev, shell);
  }

  if (!file.isDir())
    throw new Error(`ls: not a directory`);

  // file.json() fetches contentHash and parses → { filename: metadataCID }
  const dirMap = await file.json();

  if (opts.raw)
    return dirMap;

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