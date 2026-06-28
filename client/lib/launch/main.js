import { GimbalPath } from '../gimbal/path.js';

export const help = 'launch a widget';

export async function invoke(gimbal, prev, widgetName, ...args) {
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

  await window.gimbal.openWidget(mod, { name, gimbal, path, payload: prev, zone: 'master' });
}
