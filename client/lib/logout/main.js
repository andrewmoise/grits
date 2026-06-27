import { GimbalResult } from '../gimbal/result.js';

export const help = `\
logout — log out the current user

Usage:
  gimbal.logout()                         — log out all users (clears session + cookie)
  gimbal.logout('username')               — log out a specific user`;

export function invoke(gimbal, prev, username) {
  return new GimbalResult(async () => {
    await gimbal.grits.logout(gimbal._serverUrl, username || undefined);
  });
}
