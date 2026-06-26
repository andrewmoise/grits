import { GimbalResult } from '../gimbal/result.js';
import { GimbalShell } from '../gimbal/gsh.js';

export const help = `\
help — show help for a command

Usage:
  gsh.help()                       show welcome message
  gsh.help('command')              show help text for a command`;

export function invoke(prev, cmdName) {
  if (!(prev instanceof GimbalShell)) throw new Error('help: must be called on gsh');

  const shell = prev;
  if (cmdName !== undefined) {
    if (typeof cmdName !== 'string') throw new Error('help: expected a command name');
    return new GimbalResult(async () => {
      const mod = await shell._importTool(cmdName);
      return mod.help ?? `${cmdName}: no help text available`;
    });
  }

  return `Welcome to Gimbal shell!

You can use gsh.p('/path') to create path references, then chain methods:
  gsh.p('/home').ls()                        list directory
  gsh.p('/file.txt').read()                  read a file
  gsh.p('/file.txt').w('hello')              write to a file

Use gsh.help('command') for help on a specific command.`;
}
