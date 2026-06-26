import { GimbalResult } from '../gimbal/result.js';

export const help = `edit — open a file in the editor (alias for codemirror)`;

export function invoke(prev, ...args) {
  return new GimbalResult(async () => {
    const mod = await import('../codemirror/main.js');
    return mod.invoke(prev, ...args);
  });
}
