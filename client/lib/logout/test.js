export const tests = [
  {
    label: 'logout after session-only login revokes access',
    async fn(shell, scratch) {
      await shell.eval('gsh.logout()');
      await shell.eval('gsh.login()');
      const username = shell.fs._username;
      if (!username) throw new Error('no username after login');
      const vol = shell._vol(shell.serverUrl, 'primary');
      await vol.lookup('home/' + username);
      await shell.eval('gsh.logout()');
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
      await shell.eval('gsh.logout()');
      await shell.eval('gsh.login({g:1})');
      const username = shell.fs._username;
      if (!username) throw new Error('no username after login');
      const vol = shell._vol(shell.serverUrl, 'primary');
      await vol.lookup('home/' + username);
      await shell.eval('gsh.logout()');
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
      await shell.eval('gsh.logout()');
      await shell.eval('gsh.login()');
      await shell.eval('gsh.logout()');
      const text = await shell.eval('gsh.whoami()');
      if (text !== null) throw new Error(`expected null after logout, got: ${JSON.stringify(text)}`);
    },
  },
];
