export const tests = [
  {
    label: 'whoami shows null before login',
    async fn(shell, scratch) {
      await shell.eval('gsh.logout()');
      const text = await shell.eval('gsh.whoami()');
      if (text !== null) throw new Error(`expected null, got: ${JSON.stringify(text)}`);
    },
  },
  {
    label: 'whoami shows username after session-only login',
    async fn(shell, scratch) {
      await shell.eval('gsh.logout()');
      await shell.eval('gsh.login()');
      const username = shell.fs._username;
      if (!username) throw new Error('no username after login');
      const text = await shell.eval('gsh.whoami()');
      if (typeof text !== 'string') throw new Error('expected string, got null');
      const lines = text.trim().split('\n');
      if (lines.length === 0) throw new Error('whoami returned no lines');
      const entry = JSON.parse(lines[0]);
      if (entry !== username) throw new Error(`expected ${username}, got ${entry}`);
    },
  },
  {
    label: 'whoami shows username by cookie after global login',
    async fn(shell, scratch) {
      await shell.eval('gsh.logout()');
      await shell.eval('gsh.login({g:1})');
      const username = shell.fs._username;
      if (!username) throw new Error('no username after login');
      shell.fs._authToken = null;
      delete shell.fs.extraHeaders['X-Grits-Auth-Token'];
      const text = await shell.eval('gsh.whoami()');
      if (typeof text !== 'string') throw new Error('expected string, got null');
      const lines = text.trim().split('\n');
      if (lines.length === 0) throw new Error('whoami returned no lines');
      const entry = JSON.parse(lines[0]);
      if (entry !== username) throw new Error(`expected ${username}, got ${entry}`);
    },
  },
];
