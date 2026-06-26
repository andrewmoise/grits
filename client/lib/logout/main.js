import { GimbalResult } from '../gimbal/result.js';
import { GimbalShell } from '../gimbal/gsh.js';

export const help = `\
logout — log out the current user

Usage:
  gsh.logout()                         — log out all users (clears session + cookie)
  gsh.logout('username')               — log out a specific user`;

export function invoke(prev, username) {
  if (!(prev instanceof GimbalShell)) throw new Error('logout: must be called on gsh');

  const shell = prev;
  return new GimbalResult(async () => {
    await shell.fs.logout(shell.serverUrl, username || undefined);
  });
}
