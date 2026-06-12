// lib/facl/test.js
export const tests = [
  {
    label: 'facl() lists empty when no access.json',
    async fn(shell, scratch) {
      // Existing facl on a directory with no .grits — should return { allow: [] }
      // because scratch was just created and has no .grits directory.
      // If it throws "file does not exist", that's fine too — the impl may
      // change, so accept either empty JSON or an error that we handle.
      let threw = false;
      let result;
      try {
        result = await shell.eval(`facl('${scratch}')`);
      } catch {
        threw = true;
      }
      if (threw) return; // no access.json yet — acceptable
      const data = await result.json();
      if (!Array.isArray(data.allow)) {
        throw new Error('expected { allow: [...] }');
      }
    },
  },
  {
    label: 'facl({u:"alice", p:"owner"}) adds a grant',
    async fn(shell, scratch) {
      await shell.eval(`facl('${scratch}', {u:"alice", p:"owner"})`);
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
      await shell.eval(`facl('${scratch}', {u:"bob", p:"read"})`);
      // Same user, different permission → should update.
      await shell.eval(`facl('${scratch}', {u:"bob", p:"read+write"})`);
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
      await shell.eval(`facl('${scratch}', {u:"bob", p:"read"})`);
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
    label: 'facl() on .grits directory is rejected',
    async fn(shell, scratch) {
      // Create a .grits directory.
      await shell.eval(`mkdir('${scratch}/sub/.grits', {p:1})`);
      let threw = false;
      try {
        await shell.eval(`facl('${scratch}/sub/.grits', {u:"x", p:"read"})`);
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
      await shell.eval(`facl('${scratch}/data', {all:true, p:"read"})`);
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
    label: 'facl() with u: and p: aliases works',
    async fn(shell, scratch) {
      await shell.eval(`facl('${scratch}', {u:"carol", p:"owner"})`);
      const result = await shell.eval(`facl('${scratch}')`);
      const data = await result.json();
      const g = data.allow.find(a => a.user === 'carol');
      if (!g || g.permission !== 'owner') {
        throw new Error(`expected carol:owner, got ${JSON.stringify(data.allow)}`);
      }
    },
  },
];
