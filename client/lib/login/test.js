export const tests = [
  {
    label: 'clean slate before tests',
    async fn(gimbal, scratch) {
      await gimbal.eval('gimbal.logout()');
      const text = await gimbal.eval('gimbal.whoami()');
      if (text !== null) throw new Error(`expected null, got: ${JSON.stringify(text)}`);
    },
  },
  {
    label: 'session-only login grants access via header',
    async fn(gimbal, scratch) {
      await gimbal.eval('gimbal.logout()');
      await gimbal.eval('gimbal.login()');
      const username = gimbal.grits._username;
      if (!username) throw new Error('no username after login');
      const vol = gimbal.grits.volume(gimbal._serverUrl, 'primary');
      await vol.lookup('home/' + username);
    },
  },
  {
    label: 'global login grants access via cookie alone',
    async fn(gimbal, scratch) {
      await gimbal.eval('gimbal.logout()');
      await gimbal.eval('gimbal.login({g:1})');
      const username = gimbal.grits._username;
      if (!username) throw new Error('no username after login');
      gimbal.grits._authToken = null;
      delete gimbal.grits.extraHeaders['X-Grits-Auth-Token'];
      const vol = gimbal.grits.volume(gimbal._serverUrl, 'primary');
      await vol.lookup('home/' + username);
    },
  },
];
