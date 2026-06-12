import { isVoid } from '../gimbal/gsh.js';

export const help = `\
help — show help for a command

Usage:
  help()                       show welcome message
  help('command')              show help text for a command`;

export async function invoke(shell, previous, args) {
  const prev = await previous;
  if (!isVoid(prev))
    throw new Error('help: does not accept pipeline input');

  const [cmdName] = args;

  if (cmdName !== undefined) {
    if (typeof cmdName !== 'string')
      throw new Error('help: expected a command name (string), got ' + typeof cmdName);

    const mod = await shell._importTool(cmdName);
    return mod.help ?? `${cmdName}: no help text available`;
  }

  return `Welcome to Gimbal shell!

You have most basic Unix commands. cd('lib') will change directory, ls() and mkdir() and things will work. You can chain commands together; ls().to('a-file.json') will save output. Use edit(), gterm(), or files() to launch new windows.

Have a good time.`;
}