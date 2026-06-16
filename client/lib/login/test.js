// lib/login/test.js
export const tests = [
  {
    label: 'clean slate before tests',
    async fn(shell, scratch) {
      await shell.eval(`logout()`);
      const text = await shell.eval(`whoami().toText()`);
      if (!text.includes('anonymous')) throw new Error(`expected anonymous, got: ${text}`);
    },
  },
  {
    label: 'login fails with wrong password',
    async fn(shell, scratch) {
      await shell.eval(`logout()`);
      try {
        await shell.eval(`login('test', 'wrongpass')`);
        throw new Error('expected login to fail with wrong password');
      } catch (e) {
        if (e.message.includes('expected login to fail')) throw e;
      }
    },
  },
  {
    label: 'session-only login grants access via header',
    async fn(shell, scratch) {
      await shell.eval(`logout()`);
      await shell.eval(`login('test', 'test')`);
      const vol = shell._vol(shell.serverUrl, 'root');
      await vol.lookup('home/test');
    },
  },
  {
    label: 'global login grants access via cookie alone',
    async fn(shell, scratch) {
      await shell.eval(`logout()`);
      await shell.eval(`login('test', 'test', {g:1})`);
      // Simulate a fresh tab: clear the session header, rely on cookie alone
      shell.fs._authToken = null;
      delete shell.fs.extraHeaders['X-Grits-Auth-Token'];
      const vol = shell._vol(shell.serverUrl, 'root');
      await vol.lookup('home/test');
    },
  },
];
