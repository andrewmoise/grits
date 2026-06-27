import { GimbalResult } from './result.js';

export const SHORTCUTS = { r: 'read', w: 'write' };

const NON_DISPATCH = new Set(['then', 'catch', 'finally']);

export function createDispatchProxy(target, gimbal) {
  return new Proxy(target, {
    get(target, key, receiver) {
      if (typeof key === 'symbol') return Reflect.get(target, key, receiver);
      if (key in target) return Reflect.get(target, key, receiver);
      if (NON_DISPATCH.has(key)) return undefined;

      const moduleName = SHORTCUTS[key] || key;

      return (...args) => {
        return wrapResult(() => _execute(target, gimbal, moduleName, args, receiver), gimbal);
      };
    },
  });
}

export function wrapResult(executor, gimbal) {
  const result = new GimbalResult(executor);
  return createDispatchProxy(result, gimbal);
}

async function _execute(target, gimbal, moduleName, args, proxy) {
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

  const mod = await gimbal._importTool(moduleName);
  let result = mod.invoke(gimbal, prev, ...resolvedArgs);
  while (result instanceof GimbalResult) {
    result = await result;
  }
  return result;
}
