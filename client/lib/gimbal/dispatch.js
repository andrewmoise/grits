import { GimbalResult } from './result.js';

export const SHORTCUTS = { r: 'read', w: 'write', p: 'path' };

const NON_DISPATCH = new Set(['then', 'catch', 'finally']);

export function createDispatchProxy(target, shell) {
  return new Proxy(target, {
    get(target, key, receiver) {
      if (typeof key === 'symbol') return Reflect.get(target, key, receiver);
      if (key in target) return Reflect.get(target, key, receiver);
      if (NON_DISPATCH.has(key)) return undefined;

      const moduleName = SHORTCUTS[key] || key;

      return (...args) => {
        return wrapResult(() => _execute(target, shell, moduleName, args, receiver), shell);
      };
    },
  });
}

export function wrapResult(executor, shell) {
  const result = new GimbalResult(executor);
  return createDispatchProxy(result, shell);
}

async function _execute(target, shell, moduleName, args, proxy) {
  let prev;
  if (target instanceof GimbalResult) {
    prev = await target;
  } else if (proxy) {
    prev = proxy;
  } else {
    prev = target;
  }

  const resolvedArgs = await Promise.all(
    args.map(async a => a instanceof GimbalResult ? await a : a)
  );

  const mod = await shell._importTool(moduleName);
  let result = mod.invoke(prev, ...resolvedArgs);
  while (result instanceof GimbalResult) {
    result = await result;
  }
  return result;
}
