import { GimbalClient } from '../gimbal/client.js';
import { GimbalResult } from '../gimbal/result.js';

export const help = `\
logout — log out the current user

Usage:
  gimbal.logout()                         — log out all users (clears session + cookie)
  gimbal.logout('username')               — log out a specific user`;

export function invoke(gimbal, prev, username) {
  if (!(prev instanceof GimbalClient)) throw new Error('logout: must be called on gimbal');
  return new GimbalResult(async () => {
    await gimbal.grits.logout(gimbal._serverUrl, username || undefined);
  });
}
