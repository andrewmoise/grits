export const tests = [
  {
    label: 'deny({u:"bob"}) removes the grant',
    async fn(gimbal, scratch) {
      await gimbal.eval(`gimbal.p('${scratch}').allow({u:"bob", o:"*", p:"read"})`);
      await gimbal.eval(`gimbal.p('${scratch}').deny({u:"bob"})`);
      const result = await gimbal.eval(`gimbal.p('${scratch}').access()`);
      if (result.allow.length !== 0)
        throw new Error(`expected 0 grants after deny, got ${result.allow.length}`);
    },
  },
  {
    label: 'deny({}) alone clears all grants',
    async fn(gimbal, scratch) {
      await gimbal.eval(`gimbal.p('${scratch}').allow({u:"alice", o:"*", p:"read"})`);
      await gimbal.eval(`gimbal.p('${scratch}').allow({u:"bob", o:"*", p:"owner"})`);
      await gimbal.eval(`gimbal.p('${scratch}').deny({})`);
      const result = await gimbal.eval(`gimbal.p('${scratch}').access()`);
      if (result.allow.length !== 0)
        throw new Error(`expected 0 grants after clear, got ${result.allow.length}`);
    },
  },
  {
    label: 'deny() with no args clears all grants',
    async fn(gimbal, scratch) {
      await gimbal.eval(`gimbal.p('${scratch}').allow({u:"alice", o:"*", p:"read"})`);
      await gimbal.eval(`gimbal.p('${scratch}').allow({u:"bob", o:"*", p:"owner"})`);
      await gimbal.eval(`gimbal.p('${scratch}').deny()`);
      const result = await gimbal.eval(`gimbal.p('${scratch}').access()`);
      if (result.allow.length !== 0)
        throw new Error(`expected 0 grants after deny(), got ${result.allow.length}`);
    },
  },
  {
    label: 'deny({u:"nobody"}) on missing grant throws',
    async fn(gimbal, scratch) {
      let threw = false;
      try {
        await gimbal.eval(`gimbal.p('${scratch}').deny({u:"nobody"})`);
      } catch (e) {
        if (e.message.includes('grant not found')) threw = true;
        else throw e;
      }
      if (!threw) throw new Error('expected "grant not found" error');
    },
  },
  {
    label: 'deny({origin:"..."}) removes grants by origin',
    async fn(gimbal, scratch) {
      await gimbal.eval(`gimbal.p('${scratch}').allow({u:"alice", origin:"https://app.example.com", p:"read"})`);
      await gimbal.eval(`gimbal.p('${scratch}').deny({origin:"https://app.example.com"})`);
      const result = await gimbal.eval(`gimbal.p('${scratch}').access()`);
      if (result.allow.length !== 0)
        throw new Error(`expected 0 grants after origin remove, got ${result.allow.length}`);
    },
  },
  {
    label: 'deny({u, o}) removes by user and origin together',
    async fn(gimbal, scratch) {
      await gimbal.eval(`gimbal.p('${scratch}').allow({u:"alice", origin:"https://app.example.com", p:"read"})`);
      await gimbal.eval(`gimbal.p('${scratch}').allow({u:"alice", o:"*", p:"owner"})`);
      await gimbal.eval(`gimbal.p('${scratch}').deny({u:"alice", origin:"https://app.example.com"})`);
      const result = await gimbal.eval(`gimbal.p('${scratch}').access()`);
      if (result.allow.length !== 1)
        throw new Error(`expected 1 grant to remain, got ${result.allow.length}`);
      if (result.allow[0].origin !== '*')
        throw new Error(`expected alice:* to remain, got ${JSON.stringify(result.allow)}`);
    },
  },
  {
    label: 'deny() on .grits directory is rejected',
    async fn(gimbal, scratch) {
      await gimbal.eval(`gimbal.p('${scratch}/sub/.grits').mkdir({p:1})`);
      let threw = false;
      try {
        await gimbal.eval(`gimbal.p('${scratch}/sub/.grits').deny({u:"x"})`);
      } catch (e) {
        if (e.message.includes('cannot modify grants within a .grits directory')) threw = true;
        else throw e;
      }
      if (!threw) throw new Error('expected .grits rejection');
    },
  },
  {
    label: 'deny() on gimbal client throws',
    async fn(gimbal, scratch) {
      let threw = false;
      try {
        await gimbal.eval('gimbal.deny({u:"x"})');
      } catch (e) {
        if (e.message.includes('must be called on a path')) threw = true;
        else throw e;
      }
      if (!threw) throw new Error('expected "must be called on a path" error');
    },
  },
  {
    label: 'deny() with unknown key throws',
    async fn(gimbal, scratch) {
      let threw = false;
      try {
        await gimbal.eval(`gimbal.p('${scratch}').deny({x:1})`);
      } catch (e) {
        if (e.message.includes('unknown key')) threw = true;
        else throw e;
      }
      if (!threw) throw new Error('expected unknown key error');
    },
  },
  {
    label: 'deny() with too many arguments throws',
    async fn(gimbal, scratch) {
      let threw = false;
      try {
        await gimbal.eval(`gimbal.p('${scratch}').deny({u:"x"}, {o:"y"})`);
      } catch (e) {
        if (e.message.includes('too many arguments')) threw = true;
        else throw e;
      }
      if (!threw) throw new Error('expected too many arguments error');
    },
  },
];
