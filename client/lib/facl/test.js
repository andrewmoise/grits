export const tests = [
  {
    label: 'facl() returns null when no access.json',
    async fn(shell, scratch) {
      const result = await shell.eval(`gsh.facl(gsh.p('${scratch}'))`);
      if (result != null) throw new Error('expected null for directory with no ACL');
    },
  },
  {
    label: 'facl({u:"alice"}, {p:"owner"}) adds a grant',
    async fn(shell, scratch) {
      await shell.eval(`gsh.facl(gsh.p('${scratch}'), {u:"alice", o:"*"}, {p:"owner"})`);
      const result = await shell.eval(`gsh.facl(gsh.p('${scratch}'))`);
      if (!result.allow || result.allow.length !== 1)
        throw new Error(`expected 1 grant, got ${JSON.stringify(result)}`);
      const g = result.allow[0];
      if (g.user !== 'alice' || g.permission !== 'owner')
        throw new Error(`expected {user:alice, permission:owner}, got ${JSON.stringify(g)}`);
    },
  },
  {
    label: 'facl() updates existing grant when same user specified',
    async fn(shell, scratch) {
      await shell.eval(`gsh.facl(gsh.p('${scratch}'), {u:"bob", o:"*"}, {p:"read"})`);
      await shell.eval(`gsh.facl(gsh.p('${scratch}'), {u:"bob", o:"*"}, {p:"read+write"})`);
      const result = await shell.eval(`gsh.facl(gsh.p('${scratch}'))`);
      if (result.allow.length !== 1)
        throw new Error(`expected 1 grant, got ${result.allow.length}`);
      if (result.allow[0].permission !== 'read+write')
        throw new Error(`expected permission read+write, got ${result.allow[0].permission}`);
    },
  },
  {
    label: 'facl({u:"bob"}, {x:1}) removes the grant',
    async fn(shell, scratch) {
      await shell.eval(`gsh.facl(gsh.p('${scratch}'), {u:"bob", o:"*"}, {p:"read"})`);
      await shell.eval(`gsh.facl(gsh.p('${scratch}'), {u:"bob"}, {x:1})`);
      const result = await shell.eval(`gsh.facl(gsh.p('${scratch}'))`);
      if (result.allow.length !== 0)
        throw new Error(`expected 0 grants after remove, got ${result.allow.length}`);
    },
  },
  {
    label: 'facl({x:1}) on missing grant throws',
    async fn(shell, scratch) {
      let threw = false;
      try {
        await shell.eval(`gsh.facl(gsh.p('${scratch}'), {u:"nobody"}, {x:1})`);
      } catch (e) {
        if (e.message.includes('grant not found')) threw = true;
        else throw e;
      }
      if (!threw) throw new Error('expected "grant not found" error');
    },
  },
  {
    label: 'facl({x:1}) alone clears all grants',
    async fn(shell, scratch) {
      await shell.eval(`gsh.facl(gsh.p('${scratch}'), {u:"alice", o:"*"}, {p:"read"})`);
      await shell.eval(`gsh.facl(gsh.p('${scratch}'), {u:"bob", o:"*"}, {p:"owner"})`);
      await shell.eval(`gsh.facl(gsh.p('${scratch}'), {x:1})`);
      const result = await shell.eval(`gsh.facl(gsh.p('${scratch}'))`);
      if (result.allow.length !== 0)
        throw new Error(`expected 0 grants after clear, got ${result.allow.length}`);
    },
  },
  {
    label: 'facl({p:"read"}, {x:1}) removes all read grants',
    async fn(shell, scratch) {
      await shell.eval(`gsh.facl(gsh.p('${scratch}'), {u:"alice", o:"*"}, {p:"read"})`);
      await shell.eval(`gsh.facl(gsh.p('${scratch}'), {u:"bob", o:"*"}, {p:"owner"})`);
      await shell.eval(`gsh.facl(gsh.p('${scratch}'), {p:"read"}, {x:1})`);
      const result = await shell.eval(`gsh.facl(gsh.p('${scratch}'))`);
      if (result.allow.length !== 1)
        throw new Error(`expected 1 grant after removing reads, got ${result.allow.length}`);
      if (result.allow[0].user !== 'bob')
        throw new Error(`expected bob to remain, got ${JSON.stringify(result.allow)}`);
    },
  },
  {
    label: 'facl() on .grits directory is rejected',
    async fn(shell, scratch) {
      await shell.eval(`gsh.p('${scratch}/sub/.grits').mkdir({p:1})`);
      let threw = false;
      try {
        await shell.eval(`gsh.facl(gsh.p('${scratch}/sub/.grits'), {u:"x", o:"*"}, {p:"read"})`);
      } catch (e) {
        if (e.message.includes('cannot modify grants within a .grits directory')) threw = true;
        else throw e;
      }
      if (!threw) throw new Error('expected nested .grits rejection');
    },
  },
  {
    label: 'facl() on a specific path works',
    async fn(shell, scratch) {
      await shell.eval(`gsh.p('${scratch}/data').mkdir({p:1})`);
      await shell.eval(`gsh.facl(gsh.p('${scratch}/data'), {all:true, o:"*"}, {p:"read"})`);
      const result = await shell.eval(`gsh.facl(gsh.p('${scratch}/data'))`);
      if (!result.allow || result.allow.length !== 1)
        throw new Error(`expected 1 grant, got ${JSON.stringify(result)}`);
      if (result.allow[0].all !== true || result.allow[0].permission !== 'read')
        throw new Error(`expected {all:true, permission:read}, got ${JSON.stringify(result.allow[0])}`);
    },
  },
  {
    label: 'facl() with origin adds and lists origin',
    async fn(shell, scratch) {
      await shell.eval(`gsh.facl(gsh.p('${scratch}'), {u:"alice", origin:"https://app.example.com"}, {p:"owner"})`);
      const result = await shell.eval(`gsh.facl(gsh.p('${scratch}'))`);
      const g = result.allow.find(a => a.user === 'alice');
      if (!g || g.origin !== 'https://app.example.com' || g.permission !== 'owner')
        throw new Error(`expected origin & owner, got ${JSON.stringify(g)}`);
    },
  },
  {
    label: 'facl() removes by origin',
    async fn(shell, scratch) {
      await shell.eval(`gsh.facl(gsh.p('${scratch}'), {u:"alice", origin:"https://app.example.com"}, {p:"read"})`);
      await shell.eval(`gsh.facl(gsh.p('${scratch}'), {origin:"https://app.example.com"}, {x:1})`);
      const result = await shell.eval(`gsh.facl(gsh.p('${scratch}'))`);
      if (result.allow.length !== 0)
        throw new Error(`expected 0 grants after origin remove, got ${result.allow.length}`);
    },
  },
  {
    label: 'facl() without origin throws when adding',
    async fn(shell, scratch) {
      let threw = false;
      try {
        await shell.eval(`gsh.facl(gsh.p('${scratch}'), {u:"alice"}, {p:"owner"})`);
      } catch (e) {
        if (e.message.includes('origin is required')) threw = true;
        else throw e;
      }
      if (!threw) throw new Error('expected "origin is required" error');
    },
  },
  {
    label: 'facl() with {o:"*"} shorthand works',
    async fn(shell, scratch) {
      await shell.eval(`gsh.facl(gsh.p('${scratch}'), {u:"alice", o:"*"}, {p:"owner"})`);
      const result = await shell.eval(`gsh.facl(gsh.p('${scratch}'))`);
      const g = result.allow.find(a => a.user === 'alice');
      if (!g || g.origin !== '*' || g.permission !== 'owner')
        throw new Error(`expected origin:* & owner, got ${JSON.stringify(g)}`);
    },
  },
  {
    label: 'facl() with u: and p: aliases works',
    async fn(shell, scratch) {
      await shell.eval(`gsh.facl(gsh.p('${scratch}'), {u:"carol", o:"*"}, {p:"owner"})`);
      const result = await shell.eval(`gsh.facl(gsh.p('${scratch}'))`);
      const g = result.allow.find(a => a.user === 'carol');
      if (!g || g.permission !== 'owner')
        throw new Error(`expected carol:owner, got ${JSON.stringify(result.allow)}`);
    },
  },
];
