import { VOID } from '../gimbal/gsh.js';

export const help = `iframe <url> — open a URL in an iframe widget`;

export async function invoke(shell, previous, args) {
  let url = typeof args[0] === 'string' ? args[0] : 'about:blank';

  if (url !== 'about:blank' && !/^https?:\/\//i.test(url)) {
    url = 'https://' + url;
  }

  const name = (() => {
    try { return new URL(url).hostname; } catch { return url; }
  })();

  const mod = await import('./gwm-widget.js');
  await window.gimbal.openWidget(mod, {
    name,
    icon: 'iframe',
    zone: 'master',
    url,
  });

  return VOID;
}