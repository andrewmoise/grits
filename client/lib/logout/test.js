export const tests = [
  {
    label: 'logout after session-only login revokes access',
    async fn(gimbal, scratch) {
      await gimbal.eval('gimbal.logout()');
      await gimbal.eval('gimbal.login()');
      const username = gimbal.grits._username;
      if (!username) throw new Error('no username after login');
      const vol = gimbal.grits.volume(gimbal._serverUrl, 'primary');
      await vol.lookup('home/' + username);
      await gimbal.eval('gimbal.logout()');
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
    async fn(gimbal, scratch) {
      await gimbal.eval('gimbal.logout()');
      await gimbal.eval('gimbal.login({g:1})');
      const username = gimbal.grits._username;
      if (!username) throw new Error('no username after login');
      const vol = gimbal.grits.volume(gimbal._serverUrl, 'primary');
      await vol.lookup('home/' + username);
      await gimbal.eval('gimbal.logout()');
      gimbal.grits._authToken = null;
      delete gimbal.grits.extraHeaders['X-Grits-Auth-Token'];
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
    async fn(gimbal, scratch) {
      await gimbal.eval('gimbal.logout()');
      await gimbal.eval('gimbal.login()');
      await gimbal.eval('gimbal.logout()');
      const text = await gimbal.eval('gimbal.whoami()');
      if (text !== null) throw new Error(`expected null after logout, got: ${JSON.stringify(text)}`);
    },
  },
];
