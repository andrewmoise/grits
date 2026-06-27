import { GimbalResult } from '../gimbal/result.js';

export const help = `\
help — show help for a command

Usage:
  gimbal.help()                       show welcome message
  gimbal.help('command')              show help text for a command`;

export function invoke(gimbal, prev, cmdName) {
  if (cmdName !== undefined) {
    if (typeof cmdName !== 'string') throw new Error('help: expected a command name');
    return new GimbalResult(async () => {
      const mod = await gimbal._importTool(cmdName);
      return mod.help ?? `${cmdName}: no help text available`;
    });
  }

  return `Welcome to Gimbal shell!

You can use gimbal.p('/path') to create path references, then chain methods:
  gimbal.p('/home').ls()                        list directory
  gimbal.p('/file.txt').read()                  read a file
  gimbal.p('/file.txt').w('hello')              write to a file

Use gimbal.help('command') for help on a specific command.`;
}
