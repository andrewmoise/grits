import { GimbalPath } from '../gimbal/path.js';

export const help = 'launch a widget';

export async function invoke(gimbal, prev, widgetName, ...args) {
  let path = null;
  if (prev instanceof GimbalPath) {
    path = prev;
  } else if (typeof args[0] === 'string' && (args[0].startsWith('/') || args[0].startsWith('//'))) {
    path = gimbal.p(args[0]);
  }

  const mod = await import(`../${widgetName}/gwm-widget.js`);
  await window.gimbal.openWidget(mod, {
    name: widgetName,
    gimbal,
    path,
    zone: 'master',
  });
}
