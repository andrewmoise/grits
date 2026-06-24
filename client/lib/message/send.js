import { AssertionError, ASSERT_PREV_MATCHES } from '../grits/GritsClient.js';

export async function sendMessage(vol, to, from, subject, body, extra = {}) {
  const msg = {
    from,
    to,
    subject: subject || '',
    body,
    timestamp: new Date().toISOString(),
  };
  if (extra.bodyHtml) msg.bodyHtml = extra.bodyHtml;
  if (extra.bodyMarkdown) msg.bodyMarkdown = extra.bodyMarkdown;
  for (const [k, v] of Object.entries(extra)) {
    if (k === 'bodyHtml' || k === 'bodyMarkdown') continue;
    msg[k] = v;
  }

  const bytes = new TextEncoder().encode(JSON.stringify(msg));
  const destPath = `home/${to}/local/inbox`;

  const contentCID = await vol.put(bytes);
  const metaCID = await vol.mkfile(contentCID, bytes.byteLength);

  const maxAttempts = 20;
  for (let attempt = 0; attempt < maxAttempts; attempt++) {
    const filename = `${Date.now()}-${Math.random().toString(36).slice(2, 8)}.json`;
    try {
      await vol.multiLink([{
        path: `${destPath}/${filename}`,
        addr: metaCID,
        prevAddr: '',
        assert: ASSERT_PREV_MATCHES,
      }]);
      return;
    } catch (e) {
      if (!(e instanceof AssertionError)) throw e;
    }
  }

  throw new Error('could not find a unique filename after 20 attempts');
}
