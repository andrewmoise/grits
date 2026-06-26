import { GimbalResult } from '../gimbal/result.js';
import { GimbalShell } from '../gimbal/gsh.js';
import { WIDGET_ICONS } from '../style/icons.js';

export const help = `\
iframe <url> — open a URL in an iframe widget

Usage:
  gsh.iframe('https://example.com')
  gsh.iframe('example.com')          — auto-prepends https://`;

export function invoke(prev, url) {
  if (!(prev instanceof GimbalShell)) throw new Error('iframe: must be called on gsh');

  url = url || 'about:blank';
  if (url !== 'about:blank' && !/^https?:\/\//i.test(url)) url = 'https://' + url;
  const name = (() => { try { return new URL(url).hostname; } catch { return url; } })();

  return new GimbalResult(async () => {
    const mod = await import('./gwm-widget.js');
    await window.gimbal.openWidget(mod, {
      name,
      icon: WIDGET_ICONS.iframe.icon,
      iconColor: WIDGET_ICONS.iframe.iconColor,
      zone: 'master',
      url,
    });
  });
}
