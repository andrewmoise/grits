// lib/facl/test.js
export const tests = [
  {
    label: 'facl() returns void when no access.json',
    async fn(shell, scratch) {
      const result = await shell.eval(`facl('${scratch}')`);
      if (result != null && !result._gimbalVoid) {
        throw new Error('expected void for directory with no ACL');
      }
    },
  },
  {
    label: 'facl({u:"alice"}, {p:"owner"}) adds a grant',
    async fn(shell, scratch) {
      await shell.eval(`facl('${scratch}', {u:"alice", o:"*"}, {p:"owner"})`);
      const result = await shell.eval(`facl('${scratch}')`);
      const data = await result.json();
      if (!data.allow || data.allow.length !== 1) {
        throw new Error(`expected 1 grant, got ${JSON.stringify(data)}`);
      }
      const g = data.allow[0];
      if (g.user !== 'alice' || g.permission !== 'owner') {
        throw new Error(`expected {user:alice, permission:owner}, got ${JSON.stringify(g)}`);
      }
    },
  },
  {
    label: 'facl() updates existing grant when same user specified',
    async fn(shell, scratch) {
      await shell.eval(`facl('${scratch}', {u:"bob", o:"*"}, {p:"read"})`);
      await shell.eval(`facl('${scratch}', {u:"bob", o:"*"}, {p:"read+write"})`);
      const result = await shell.eval(`facl('${scratch}')`);
      const data = await result.json();
      if (data.allow.length !== 1) {
        throw new Error(`expected 1 grant, got ${data.allow.length}`);
      }
      if (data.allow[0].permission !== 'read+write') {
        throw new Error(`expected permission read+write, got ${data.allow[0].permission}`);
      }
    },
  },
  {
    label: 'facl({u:"bob"}, {x:1}) removes the grant',
    async fn(shell, scratch) {
      await shell.eval(`facl('${scratch}', {u:"bob", o:"*"}, {p:"read"})`);
      await shell.eval(`facl('${scratch}', {u:"bob"}, {x:1})`);
      const result = await shell.eval(`facl('${scratch}')`);
      const data = await result.json();
      if (data.allow.length !== 0) {
        throw new Error(`expected 0 grants after remove, got ${data.allow.length}`);
      }
    },
  },
  {
    label: 'facl({x:1}) on missing grant throws',
    async fn(shell, scratch) {
      let threw = false;
      try {
        await shell.eval(`facl('${scratch}', {u:"nobody"}, {x:1})`);
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
      await shell.eval(`facl('${scratch}', {u:"alice", o:"*"}, {p:"read"})`);
      await shell.eval(`facl('${scratch}', {u:"bob", o:"*"}, {p:"owner"})`);
      await shell.eval(`facl('${scratch}', {x:1})`);
      const result = await shell.eval(`facl('${scratch}')`);
      const data = await result.json();
      if (data.allow.length !== 0) {
        throw new Error(`expected 0 grants after clear, got ${data.allow.length}`);
      }
    },
  },
  {
    label: 'facl({p:"read"}, {x:1}) removes all read grants',
    async fn(shell, scratch) {
      await shell.eval(`facl('${scratch}', {u:"alice", o:"*"}, {p:"read"})`);
      let dump = await (await shell.eval(`facl('${scratch}')`)).json();
      await shell.eval(`facl('${scratch}', {u:"bob", o:"*"}, {p:"owner"})`);
      dump = await (await shell.eval(`facl('${scratch}')`)).json();
      await shell.eval(`facl('${scratch}', {p:"read"}, {x:1})`);
      dump = await (await shell.eval(`facl('${scratch}')`)).json();
      const result = await shell.eval(`facl('${scratch}')`);
      const data = await result.json();
      if (data.allow.length !== 1) {
        throw new Error(`expected 1 grant after removing reads, got ${data.allow.length}`);
      }
      if (data.allow[0].user !== 'bob') {
        throw new Error(`expected bob to remain, got ${JSON.stringify(data.allow)}`);
      }
    },
  },
  {
    label: 'facl() on .grits directory is rejected',
    async fn(shell, scratch) {
      await shell.eval(`mkdir('${scratch}/sub/.grits', {p:1})`);
      let threw = false;
      try {
        await shell.eval(`facl('${scratch}/sub/.grits', {u:"x", o:"*"}, {p:"read"})`);
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
      await shell.eval(`mkdir('${scratch}/data', {p:1})`);
      await shell.eval(`facl('${scratch}/data', {all:true, o:"*"}, {p:"read"})`);
      const result = await shell.eval(`facl('${scratch}/data')`);
      const data = await result.json();
      if (!data.allow || data.allow.length !== 1) {
        throw new Error(`expected 1 grant, got ${JSON.stringify(data)}`);
      }
      if (data.allow[0].all !== true || data.allow[0].permission !== 'read') {
        throw new Error(`expected {all:true, permission:read}, got ${JSON.stringify(data.allow[0])}`);
      }
    },
  },
  {
    label: 'facl() with origin adds and lists origin',
    async fn(shell, scratch) {
      await shell.eval(`facl('${scratch}', {u:"alice", origin:"https://app.example.com"}, {p:"owner"})`);
      const result = await shell.eval(`facl('${scratch}')`);
      const data = await result.json();
      const g = data.allow.find(a => a.user === 'alice');
      if (!g || g.origin !== 'https://app.example.com' || g.permission !== 'owner') {
        throw new Error(`expected origin & owner, got ${JSON.stringify(g)}`);
      }
    },
  },
  {
    label: 'facl() removes by origin',
    async fn(shell, scratch) {
      await shell.eval(`facl('${scratch}', {u:"alice", origin:"https://app.example.com"}, {p:"read"})`);
      await shell.eval(`facl('${scratch}', {origin:"https://app.example.com"}, {x:1})`);
      const result = await shell.eval(`facl('${scratch}')`);
      const data = await result.json();
      if (data.allow.length !== 0) {
        throw new Error(`expected 0 grants after origin remove, got ${data.allow.length}`);
      }
    },
  },
  {
    label: 'facl() without origin throws when adding',
    async fn(shell, scratch) {
      let threw = false;
      try {
        await shell.eval(`facl('${scratch}', {u:"alice"}, {p:"owner"})`);
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
      await shell.eval(`facl('${scratch}', {u:"alice", o:"*"}, {p:"owner"})`);
      const result = await shell.eval(`facl('${scratch}')`);
      const data = await result.json();
      const g = data.allow.find(a => a.user === 'alice');
      if (!g || g.origin !== '*' || g.permission !== 'owner') {
        throw new Error(`expected origin:* & owner, got ${JSON.stringify(g)}`);
      }
    },
  },
  {
    label: 'facl() with u: and p: aliases works',
    async fn(shell, scratch) {
      await shell.eval(`facl('${scratch}', {u:"carol", o:"*"}, {p:"owner"})`);
      const result = await shell.eval(`facl('${scratch}')`);
      const data = await result.json();
      const g = data.allow.find(a => a.user === 'carol');
      if (!g || g.permission !== 'owner') {
        throw new Error(`expected carol:owner, got ${JSON.stringify(data.allow)}`);
      }
    },
  },
];
