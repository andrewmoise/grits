import { GimbalPath } from '../gimbal/path.js';
import { GimbalResult } from '../gimbal/result.js';

export const help = `\
click — open a file in an appropriate widget based on extension

Usage:
  dir.click(relPath)    opens relPath relative to dir

Files ending in .md open in the markdown viewer; everything else opens in the text editor.`;

export function invoke(gimbal, prev, ...args) {
  if (!(prev instanceof GimbalPath))
    throw new Error('click: must be called on a path');

  let relPath, opts = {};
  for (let i = 0; i < args.length; i++) {
    const a = args[i];
    if (i === 0 && typeof a === 'string') {
      relPath = a;
    } else if (i === args.length - 1 && typeof a === 'object' && !(a instanceof GimbalPath) && !(a instanceof GimbalResult)) {
      opts = a;
    } else {
      throw new Error('click: unexpected argument');
    }
  }

  if (!relPath)
    throw new Error('click: a relative path argument is required');

  return new GimbalResult(async () => {
    const target = prev.p(relPath);
    console.log(`  target: ${target}`)
    if (relPath.endsWith('.md')) {
      console.log('  md');
      await target.launch('markdown');
    } else {
      console.log('  edit');
      await target.launch('edit');
    }
    return null;
  });
}
