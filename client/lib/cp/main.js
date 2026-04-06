// lib/cp/main.js
export const help = `\
cp — copy a file to a new path

Usage:
  cp('src', 'dest')          copy into dest if dir, overwrite if file
  cp('src', 'dest/')         dest must be a directory, place inside it
  cp('src', 'dest', {f:1})   overwrite even if dest is a directory
  cp('src', 'dest', {i:1})   fail if dest exists at all`;

import { invoke as lnInvoke } from '../ln/main.js';

export async function invoke(shell, previous, args) {
  return lnInvoke(shell, previous, args, 'cp');
}