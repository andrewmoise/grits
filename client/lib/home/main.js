import { GimbalResult } from '../gimbal/result.js';
import { GimbalPath } from '../gimbal/path.js';
import { GimbalShell } from '../gimbal/gsh.js';

export const help = `\
home — return a GimbalPath to the current user's home directory

Usage:
  gsh.home()               returns p(/home/{username})

Convenience shortcut for gsh.p('/home/{username}').`;

export function invoke(prev) {
  if (!(prev instanceof GimbalShell)) throw new Error('home: must be called on gsh');

  const shell = prev;
  return new GimbalResult(async () => {
    const identities = await shell.fs.whoami(shell.serverUrl);
    const username = identities?.[0]?.username;
    if (!username) throw new Error('home: not logged in');
    return new GimbalPath('/home/' + username, shell);
  });
}
