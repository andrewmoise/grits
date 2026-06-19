// lib/logout/test.js
export const tests = [
  {
    label: 'logout after session-only login revokes access',
    async fn(shell, scratch) {
      await shell.eval(`logout()`);
      await shell.eval(`login()`);
      const username = shell.fs._username;
      if (!username) throw new Error('no username after login');
      const vol = shell._vol(shell.serverUrl, 'primary');
      await vol.lookup('home/' + username);
      await shell.eval(`logout()`);
      try {
        await vol.lookup('home/' + username);
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
      await shell.eval(`login({g:1})`);
      const username = shell.fs._username;
      if (!username) throw new Error('no username after login');
      const vol = shell._vol(shell.serverUrl, 'primary');
      await vol.lookup('home/' + username);
      await shell.eval(`logout()`);
      // Simulate fresh tab — no session header
      shell.fs._authToken = null;
      delete shell.fs.extraHeaders['X-Grits-Auth-Token'];
      try {
        await vol.lookup('home/' + username);
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
      await shell.eval(`login()`);
      await shell.eval(`logout()`);
      const text = await shell.eval(`whoami().toText()`);
      if (text !== '') throw new Error(`expected empty after logout, got: ${JSON.stringify(text)}`);
    },
  },
];
