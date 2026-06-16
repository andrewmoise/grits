// lib/logout/test.js
export const tests = [
  {
    label: 'logout after session-only login revokes access',
    async fn(shell, scratch) {
      await shell.eval(`logout()`);
      await shell.eval(`login('test', 'test')`);
      const vol = shell._vol(shell.serverUrl, 'root');
      await vol.lookup('home/test');
      await shell.eval(`logout()`);
      try {
        await vol.lookup('home/test');
        throw new Error('expected access_denied after logout');
      } catch (e) {
        if (e.message.includes('expected access_denied')) throw e;
        if (!e.message.includes('denied') && !e.message.includes('403'))
          throw new Error(`unexpected error after logout: ${e.message}`);
      }
    },
  },
  {
    label: 'logout after global login revokes cookie access',
    async fn(shell, scratch) {
      await shell.eval(`logout()`);
      await shell.eval(`login('test', 'test', {g:1})`);
      const vol = shell._vol(shell.serverUrl, 'root');
      await vol.lookup('home/test');
      await shell.eval(`logout()`);
      // Simulate fresh tab — no session header
      shell.fs._authToken = null;
      delete shell.fs.extraHeaders['X-Grits-Auth-Token'];
      try {
        await vol.lookup('home/test');
        throw new Error('expected access_denied after logout of global session');
      } catch (e) {
        if (e.message.includes('expected access_denied')) throw e;
        if (!e.message.includes('denied') && !e.message.includes('403'))
          throw new Error(`unexpected error after logout: ${e.message}`);
      }
    },
  },
  {
    label: 'logout clears session token',
    async fn(shell, scratch) {
      await shell.eval(`logout()`);
      await shell.eval(`login('test', 'test')`);
      await shell.eval(`logout()`);
      const text = await shell.eval(`whoami().toText()`);
      if (!text.includes('anonymous')) throw new Error(`expected anonymous after logout, got: ${text}`);
    },
  },
];
