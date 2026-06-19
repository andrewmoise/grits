// lib/login/test.js
export const tests = [
  {
    label: 'clean slate before tests',
    async fn(shell, scratch) {
      await shell.eval(`logout()`);
      const text = await shell.eval(`whoami().toText()`);
      if (text !== '') throw new Error(`expected empty, got: ${JSON.stringify(text)}`);
    },
  },
  {
    label: 'session-only login grants access via header',
    async fn(shell, scratch) {
      await shell.eval(`logout()`);
      await shell.eval(`login()`);
      const username = shell.fs._username;
      if (!username) throw new Error('no username after login');
      const vol = shell._vol(shell.serverUrl, 'primary');
      await vol.lookup('home/' + username);
    },
  },
  {
    label: 'global login grants access via cookie alone',
    async fn(shell, scratch) {
      await shell.eval(`logout()`);
      await shell.eval(`login({g:1})`);
      const username = shell.fs._username;
      if (!username) throw new Error('no username after login');
      // Simulate a fresh tab: clear the session header, rely on cookie alone
      shell.fs._authToken = null;
      delete shell.fs.extraHeaders['X-Grits-Auth-Token'];
      const vol = shell._vol(shell.serverUrl, 'primary');
      await vol.lookup('home/' + username);
    },
  },
];
