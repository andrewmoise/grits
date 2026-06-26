export const tests = [
  {
    label: 'clean slate before tests',
    async fn(shell, scratch) {
      await shell.eval('gsh.logout()');
      const text = await shell.eval('gsh.whoami()');
      if (text !== null) throw new Error(`expected null, got: ${JSON.stringify(text)}`);
    },
  },
  {
    label: 'session-only login grants access via header',
    async fn(shell, scratch) {
      await shell.eval('gsh.logout()');
      await shell.eval('gsh.login()');
      const username = shell.fs._username;
      if (!username) throw new Error('no username after login');
      const vol = shell._vol(shell.serverUrl, 'primary');
      await vol.lookup('home/' + username);
    },
  },
  {
    label: 'global login grants access via cookie alone',
    async fn(shell, scratch) {
      await shell.eval('gsh.logout()');
      await shell.eval('gsh.login({g:1})');
      const username = shell.fs._username;
      if (!username) throw new Error('no username after login');
      shell.fs._authToken = null;
      delete shell.fs.extraHeaders['X-Grits-Auth-Token'];
      const vol = shell._vol(shell.serverUrl, 'primary');
      await vol.lookup('home/' + username);
    },
  },
];
