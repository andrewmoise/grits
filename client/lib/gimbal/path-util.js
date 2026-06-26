import { GimbalPath } from './path.js';
import { GimbalShell } from './gsh.js';
import { GimbalResult } from './result.js';

export function resolvePathArg(prev, args) {
  if (prev instanceof GimbalPath) return prev;

  if (prev instanceof GimbalResult && typeof prev.then === 'function') {
    return new GimbalResult(async () => {
      const resolved = await prev;
      return resolvePathArg(resolved, args);
    });
  }

  if (prev instanceof GimbalShell) {
    const path = args.find(a => a instanceof GimbalPath);
    if (path) return path;

    const str = args.find(a => typeof a === 'string');
    if (str) {
      const r = prev.resolvePath(str);
      return new GimbalPath('/' + r.path, prev);
    }

    return null;
  }

  return null;
}
