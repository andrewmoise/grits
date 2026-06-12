// lib/login/main.js
export const help = `\
login — authenticate with a username and password

Usage:
  login('username', 'password')`;

import { isVoid, responseFromText } from '../gimbal/gsh.js';

export async function invoke(shell, previous, args, cmd = 'login') {
  const prev = await previous;
  if (!isVoid(prev))
    throw new Error(`${cmd}: does not accept pipeline input`);

  const [username, password] = args;
  if (!username || !password)
    throw new Error(`${cmd}: usage: login('username', 'password')`);

  await shell.fs.login(shell.serverUrl, username, password);
  return responseFromText(`logged in as ${username}`);
}
