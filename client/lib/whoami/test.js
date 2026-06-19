// lib/whoami/test.js
export const tests = [
  {
    label: 'whoami shows empty before login',
    async fn(shell, scratch) {
      await shell.eval(`logout()`);
      const text = await shell.eval(`whoami().toText()`);
      if (text !== '') throw new Error(`expected empty, got: ${JSON.stringify(text)}`);
    },
  },
  {
    label: 'whoami shows username after session-only login',
    async fn(shell, scratch) {
      await shell.eval(`logout()`);
      await shell.eval(`login()`);
      const username = shell.fs._username;
      if (!username) throw new Error('no username after login');
      const text = await shell.eval(`whoami().toText()`);
      const lines = text.trim().split('\n');
      if (lines.length === 0) throw new Error('whoami returned no lines');
      const entry = JSON.parse(lines[0]);
      if (entry !== username) throw new Error(`expected ${username}, got ${entry}`);
    },
  },
  {
    label: 'whoami shows username by cookie after global login',
    async fn(shell, scratch) {
      await shell.eval(`logout()`);
      await shell.eval(`login({g:1})`);
      const username = shell.fs._username;
      if (!username) throw new Error('no username after login');
      // Simulate fresh tab — no session header, rely on cookie
      shell.fs._authToken = null;
      delete shell.fs.extraHeaders['X-Grits-Auth-Token'];
      const text = await shell.eval(`whoami().toText()`);
      const lines = text.trim().split('\n');
      if (lines.length === 0) throw new Error('whoami returned no lines');
      const entry = JSON.parse(lines[0]);
      if (entry !== username) throw new Error(`expected ${username}, got ${entry}`);
    },
  },
];
