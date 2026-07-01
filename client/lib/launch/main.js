import { GimbalPath } from '../gimbal/path.js';

export const help = 'launch a widget';

function isPlainObject(v) {
  if (!v || typeof v !== 'object') return false;
  const p = Object.getPrototypeOf(v);
  return p === Object.prototype || p === null;
}

export async function invoke(gimbal, prev, widgetName, ...args) {
  const rawOpts = args.length > 0 && isPlainObject(args[args.length - 1]) ? args.pop() : {};
  const { iconColor, ...restOpts } = rawOpts;
  if (Object.keys(restOpts).length) throw new Error(`launch: unknown option "${Object.keys(restOpts)[0]}"`);

  let path = null;
  if (prev instanceof GimbalPath) {
    path = prev;
  }

  let mod, name;
  if (widgetName instanceof GimbalPath) {
    const r = gimbal.resolvePath(widgetName.abs());
    const url = `${gimbal._serverUrl}/grits/v1/content/${r.volumeName}/${r.path}`;
    mod = await import(url);
    name = widgetName.toString();
    if (!path && args[0] instanceof GimbalPath) path = args[0];
  } else {
    mod = await import(`../${widgetName}/gwm-widget.js`);
    name = widgetName;
    if (!path && typeof args[0] === 'string' && (args[0].startsWith('/') || args[0].startsWith('//')))
      path = gimbal.p(args[0]);
  }

  await window.gimbal.openWidget(mod, { name, gimbal, path, payload: prev, zone: 'master', iconColor });
}
