import { VOID, isVoid } from '../gimbal/gsh.js';

export const help = `\
help — show getting started message

Usage:
  help()`;

export async function invoke(shell, previous, args) {
  const prev = await previous;
  if (!isVoid(prev))
    throw new Error('help: does not accept pipeline input');

  return `Welcome to Gimbal shell!

You have most basic Unix commands. cd('lib') will change directory, ls() and mkdir() and things will work. You can chain commands together; ls().to('a-file.json') will save output. Use edit(), gterm(), or files() to launch new windows.

Have a good time.`;
}