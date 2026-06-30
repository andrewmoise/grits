export const tests = [
  {
    label: 'allow({u, o, p}) adds a grant',
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
    label: 'allow() updates existing grant when same user+origin specified',
    async fn(gimbal, scratch) {
      await gimbal.eval(`gimbal.p('${scratch}').allow({u:"bob", o:"*", p:"read"})`);
      await gimbal.eval(`gimbal.p('${scratch}').allow({u:"bob", o:"*", p:"read+write"})`);
      const result = await gimbal.eval(`gimbal.p('${scratch}').access()`);
      if (result.allow.length !== 1)
        throw new Error(`expected 1 grant, got ${result.allow.length}`);
      if (result.allow[0].permission !== 'read+write')
        throw new Error(`expected permission read+write, got ${result.allow[0].permission}`);
    },
  },
  {
    label: 'allow() with origin adds and persists origin',
    async fn(gimbal, scratch) {
      await gimbal.eval(`gimbal.p('${scratch}').allow({u:"alice", origin:"https://app.example.com", p:"owner"})`);
      const result = await gimbal.eval(`gimbal.p('${scratch}').access()`);
      const g = result.allow.find(a => a.user === 'alice');
      if (!g || g.origin !== 'https://app.example.com' || g.permission !== 'owner')
        throw new Error(`expected origin & owner, got ${JSON.stringify(g)}`);
    },
  },
  {
    label: 'allow() on .grits directory is rejected',
    async fn(gimbal, scratch) {
      await gimbal.eval(`gimbal.p('${scratch}/sub/.grits').mkdir({p:1})`);
      let threw = false;
      try {
        await gimbal.eval(`gimbal.p('${scratch}/sub/.grits').allow({u:"x", o:"*", p:"read"})`);
      } catch (e) {
        if (e.message.includes('cannot modify grants within a .grits directory')) threw = true;
        else throw e;
      }
      if (!threw) throw new Error('expected .grits rejection');
    },
  },
  {
    label: 'allow() without origin throws',
    async fn(gimbal, scratch) {
      let threw = false;
      try {
        await gimbal.eval(`gimbal.p('${scratch}').allow({u:"alice", p:"owner"})`);
      } catch (e) {
        if (e.message.includes('origin (o) is required')) threw = true;
        else throw e;
      }
      if (!threw) throw new Error('expected "origin is required" error');
    },
  },
  {
    label: 'allow() without u throws',
    async fn(gimbal, scratch) {
      let threw = false;
      try {
        await gimbal.eval(`gimbal.p('${scratch}').allow({o:"*", p:"owner"})`);
      } catch (e) {
        if (e.message.includes('specify a principal')) threw = true;
        else throw e;
      }
      if (!threw) throw new Error('expected "specify a principal" error');
    },
  },
  {
    label: 'allow() without p throws',
    async fn(gimbal, scratch) {
      let threw = false;
      try {
        await gimbal.eval(`gimbal.p('${scratch}').allow({u:"alice", o:"*"})`);
      } catch (e) {
        if (e.message.includes('permission (p) is required')) threw = true;
        else throw e;
      }
      if (!threw) throw new Error('expected "permission (p) is required" error');
    },
  },
  {
    label: 'allow() with {o:"*"} shorthand works',
    async fn(gimbal, scratch) {
      await gimbal.eval(`gimbal.p('${scratch}').allow({u:"alice", o:"*", p:"owner"})`);
      const result = await gimbal.eval(`gimbal.p('${scratch}').access()`);
      const g = result.allow.find(a => a.user === 'alice');
      if (!g || g.origin !== '*' || g.permission !== 'owner')
        throw new Error(`expected origin:* & owner, got ${JSON.stringify(g)}`);
    },
  },
  {
    label: 'allow() with aliases {user, origin, permission} works',
    async fn(gimbal, scratch) {
      await gimbal.eval(`gimbal.p('${scratch}').allow({user:"carol", origin:"*", permission:"owner"})`);
      const result = await gimbal.eval(`gimbal.p('${scratch}').access()`);
      const g = result.allow.find(a => a.user === 'carol');
      if (!g || g.permission !== 'owner')
        throw new Error(`expected carol:owner, got ${JSON.stringify(result.allow)}`);
    },
  },
  {
    label: 'allow() on gimbal client throws',
    async fn(gimbal, scratch) {
      let threw = false;
      try {
        await gimbal.eval('gimbal.allow({u:"x", o:"*", p:"read"})');
      } catch (e) {
        if (e.message.includes('must be called on a path')) threw = true;
        else throw e;
      }
      if (!threw) throw new Error('expected "must be called on a path" error');
    },
  },
  {
    label: 'allow() with unknown key throws',
    async fn(gimbal, scratch) {
      let threw = false;
      try {
        await gimbal.eval(`gimbal.p('${scratch}').allow({u:"x", o:"*", p:"read", x:1})`);
      } catch (e) {
        if (e.message.includes('unknown key')) threw = true;
        else throw e;
      }
      if (!threw) throw new Error('expected unknown key error');
    },
  },
];
