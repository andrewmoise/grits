// lib/whoami/test.js
export const tests = [
  {
    label: 'whoami shows anonymous before login',
    async fn(shell, scratch) {
      await shell.eval(`logout()`);
      const text = await shell.eval(`whoami().toText()`);
      if (!text.includes('anonymous')) throw new Error(`expected anonymous, got: ${text}`);
    },
  },
  {
    label: 'whoami shows username after session-only login',
    async fn(shell, scratch) {
      await shell.eval(`logout()`);
      await shell.eval(`login('test', 'test')`);
      const text = await shell.eval(`whoami().toText()`);
      if (!text.includes('test')) throw new Error(`expected username in whoami, got: ${text}`);
    },
  },
  {
    label: 'whoami shows username by cookie after global login',
    async fn(shell, scratch) {
      await shell.eval(`logout()`);
      await shell.eval(`login('test', 'test', {g:1})`);
      // Simulate fresh tab — no session header, rely on cookie
      shell.fs._authToken = null;
      delete shell.fs.extraHeaders['X-Grits-Auth-Token'];
      const text = await shell.eval(`whoami().toText()`);
      if (!text.includes('test')) throw new Error(`expected username via cookie in whoami, got: ${text}`);
    },
  },
];
