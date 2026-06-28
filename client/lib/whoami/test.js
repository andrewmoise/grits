export const tests = [
  {
    label: 'whoami shows null before login',
    async fn(gimbal, scratch) {
      await gimbal.eval('gimbal.logout()');
      const text = await gimbal.eval('gimbal.whoami()');
      if (text !== null) throw new Error(`expected null, got: ${JSON.stringify(text)}`);
    },
  },
  {
    label: 'whoami shows username after session-only login',
    async fn(gimbal, scratch) {
      await gimbal.eval('gimbal.logout()');
      await gimbal.eval('gimbal.login()');
      const username = gimbal.grits._username;
      if (!username) throw new Error('no username after login');
      const text = await gimbal.eval('gimbal.whoami()');
      if (typeof text !== 'string') throw new Error('expected string, got null');
      const lines = text.trim().split('\n');
      if (lines.length === 0) throw new Error('whoami returned no lines');
      if (lines[0] !== username) throw new Error(`expected ${username}, got ${lines[0]}`);
    },
  },
  {
    label: 'whoami shows username by cookie after global login',
    async fn(gimbal, scratch) {
      await gimbal.eval('gimbal.logout()');
      await gimbal.eval('gimbal.login({g:1})');
      const username = gimbal.grits._username;
      if (!username) throw new Error('no username after login');
      gimbal.grits._authToken = null;
      delete gimbal.grits.extraHeaders['X-Grits-Auth-Token'];
      const text = await gimbal.eval('gimbal.whoami()');
      if (typeof text !== 'string') throw new Error('expected string, got null');
      const lines = text.trim().split('\n');
      if (lines.length === 0) throw new Error('whoami returned no lines');
      if (lines[0] !== username) throw new Error(`expected ${username}, got ${lines[0]}`);
    },
  },
];
