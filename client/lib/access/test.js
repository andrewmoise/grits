export const tests = [
  {
    label: 'access() returns null when no access.json',
    async fn(gimbal, scratch) {
      const result = await gimbal.eval(`gimbal.p('${scratch}').access()`);
      if (result != null) throw new Error('expected null for directory with no ACL');
    },
  },
  {
    label: 'access() after adding a grant shows it',
    async fn(gimbal, scratch) {
      await gimbal.eval(`gimbal.p('${scratch}').allow({u:"alice", o:"*", p:"owner"})`);
      const result = await gimbal.eval(`gimbal.p('${scratch}').access()`);
      if (!result.allow || result.allow.length !== 1)
        throw new Error(`expected 1 grant, got ${JSON.stringify(result)}`);
      const g = result.allow[0];
      if (g.user !== 'alice' || g.permission !== 'owner')
        throw new Error(`expected {user:alice, permission:owner}, got ${JSON.stringify(g)}`);
    },
  },
  {
    label: 'access() on a subdirectory path works',
    async fn(gimbal, scratch) {
      await gimbal.eval(`gimbal.p('${scratch}/data').mkdir({p:1})`);
      await gimbal.eval(`gimbal.p('${scratch}/data').allow({all:true, o:"*", p:"read"})`);
      const result = await gimbal.eval(`gimbal.p('${scratch}/data').access()`);
      if (!result.allow || result.allow.length !== 1)
        throw new Error(`expected 1 grant, got ${JSON.stringify(result)}`);
      if (result.allow[0].all !== true || result.allow[0].permission !== 'read')
        throw new Error(`expected {all:true, permission:read}, got ${JSON.stringify(result.allow[0])}`);
    },
  },
  {
    label: 'access() with origin shows origin',
    async fn(gimbal, scratch) {
      await gimbal.eval(`gimbal.p('${scratch}').allow({u:"alice", origin:"https://app.example.com", p:"owner"})`);
      const result = await gimbal.eval(`gimbal.p('${scratch}').access()`);
      const g = result.allow.find(a => a.user === 'alice');
      if (!g || g.origin !== 'https://app.example.com' || g.permission !== 'owner')
        throw new Error(`expected origin & owner, got ${JSON.stringify(g)}`);
    },
  },
  {
    label: 'access() with {o:"*"} shorthand shows origin *',
    async fn(gimbal, scratch) {
      await gimbal.eval(`gimbal.p('${scratch}').allow({u:"alice", o:"*", p:"owner"})`);
      const result = await gimbal.eval(`gimbal.p('${scratch}').access()`);
      const g = result.allow.find(a => a.user === 'alice');
      if (!g || g.origin !== '*' || g.permission !== 'owner')
        throw new Error(`expected origin:* & owner, got ${JSON.stringify(g)}`);
    },
  },
  {
    label: 'access() on gimbal client throws',
    async fn(gimbal, scratch) {
      let threw = false;
      try {
        await gimbal.eval('gimbal.access()');
      } catch (e) {
        if (e.message.includes('must be called on a path')) threw = true;
        else throw e;
      }
      if (!threw) throw new Error('expected "must be called on a path" error');
    },
  },
  {
    label: 'access() with arguments throws',
    async fn(gimbal, scratch) {
      let threw = false;
      try {
        await gimbal.eval(`gimbal.p('${scratch}').access('extra')`);
      } catch (e) {
        if (e.message.includes('does not accept arguments')) threw = true;
        else throw e;
      }
      if (!threw) throw new Error('expected "does not accept arguments" error');
    },
  },
];
