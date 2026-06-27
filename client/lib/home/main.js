import { GimbalResult } from '../gimbal/result.js';

export const help = `\
home — return a GimbalPath to the current user's home directory

Usage:
  gimbal.home()               returns p(/home/{username})

Convenience shortcut for gimbal.p('/home/{username}').`;

export function invoke(gimbal, prev) {
  return new GimbalResult(async () => {
    const identities = await gimbal.grits.whoami(gimbal._serverUrl);
    const username = identities?.[0]?.username;
    if (!username) throw new Error('home: not logged in');
    return gimbal.p('/home/' + username);
  });
}
