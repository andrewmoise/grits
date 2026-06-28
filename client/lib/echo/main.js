import { GimbalClient } from '../gimbal/client.js';
import { GimbalResult } from '../gimbal/result.js';

export const help = `\
echo — pass a value through the pipeline as-is

Usage:
  gimbal.echo(value)        returns value as prev for chaining

Accepts any value. Useful for injecting arbitrary values into a pipeline:
  echo({a:1}).json()        →  '{"a":1}'
  echo(null).json()          →  'null'`;

export function invoke(gimbal, prev, ...args) {
  if (!(prev instanceof GimbalClient))
    throw new Error(`echo: must be called on gimbal`);

  return new GimbalResult(() => args[0]);
}
