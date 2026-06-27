export const tests = [
  {
    label: 'mkdir creates a directory',
    async fn(gimbal, scratch) {
      await gimbal.eval(`gimbal.p('${scratch}/newdir').mkdir()`);
      const r = gimbal.resolvePath(`${scratch}/newdir`);
      const file = await gimbal.grits.volume(gimbal._serverUrl, r.volumeName).lookup(r.path);
      if (!file.isDir()) throw new Error('expected a directory');
    },
  },
  {
    label: 'mkdir fails if path already exists',
    async fn(gimbal, scratch) {
      await gimbal.eval(`gimbal.p('${scratch}/newdir').mkdir()`);
      let threw = false;
      try {
        await gimbal.eval(`gimbal.p('${scratch}/newdir').mkdir()`);
      } catch (e) {
        if (e.message.includes('already exists')) threw = true;
        else throw e;
      }
      if (!threw) throw new Error('expected already-exists error');
    },
  },
  {
    label: 'mkdir with {f:1} succeeds if already exists',
    async fn(gimbal, scratch) {
      await gimbal.eval(`gimbal.p('${scratch}/newdir').mkdir()`);
      await gimbal.eval(`gimbal.p('${scratch}/newdir').mkdir({f:1})`);
      const r = gimbal.resolvePath(`${scratch}/newdir`);
      const file = await gimbal.grits.volume(gimbal._serverUrl, r.volumeName).lookup(r.path);
      if (!file.isDir()) throw new Error('expected a directory');
    },
  },
  {
    label: 'mkdir does not accept pipeline input',
    async fn(gimbal, scratch) {
      await gimbal.eval(`gimbal.p('${scratch}/afile.txt').w('x')`);
      let threw = false;
      try {
        await gimbal.eval(`gimbal.p('${scratch}/afile.txt').read().mkdir({p:1})`);
      } catch (e) {
        if (e.message.includes('need a path')) threw = true;
        else throw e;
      }
      if (!threw) throw new Error('expected path error');
    },
  },
  {
    label: 'mkdir requires a path argument',
    async fn(gimbal, scratch) {
      let threw = false;
      try {
        await gimbal.eval('gimbal.mkdir()');
      } catch (e) {
        if (e.message.includes('need a path')) threw = true;
        else throw e;
      }
      if (!threw) throw new Error('expected usage error');
    },
  },
  {
    label: 'mkdir {p:1} creates intermediate directories',
    async fn(gimbal, scratch) {
      await gimbal.eval(`gimbal.p('${scratch}/a/b/c').mkdir({p:1})`);
      const r = gimbal.resolvePath(`${scratch}/a/b/c`);
      const file = await gimbal.grits.volume(gimbal._serverUrl, r.volumeName).lookup(r.path);
      if (!file.isDir()) throw new Error('expected a directory');
    },
  },
  {
    label: 'mkdir {p:1} succeeds if directories already exist',
    async fn(gimbal, scratch) {
      await gimbal.eval(`gimbal.p('${scratch}/a/b').mkdir({p:1})`);
      await gimbal.eval(`gimbal.p('${scratch}/a/b').mkdir({p:1})`);
      const r = gimbal.resolvePath(`${scratch}/a/b`);
      const file = await gimbal.grits.volume(gimbal._serverUrl, r.volumeName).lookup(r.path);
      if (!file.isDir()) throw new Error('expected a directory');
    },
  },
  {
    label: 'mkdir {f:1,p:1} replaces file with directory',
    async fn(gimbal, scratch) {
      await gimbal.eval(`gimbal.p('${scratch}/a').w('hi')`);
      await gimbal.eval(`gimbal.p('${scratch}/a/b').mkdir({f:1,p:1})`);
      const rA = gimbal.resolvePath(`${scratch}/a`);
      const fileA = await gimbal.grits.volume(gimbal._serverUrl, rA.volumeName).lookup(rA.path);
      if (!fileA.isDir()) throw new Error('expected a to be a directory');
      const rB = gimbal.resolvePath(`${scratch}/a/b`);
      const fileB = await gimbal.grits.volume(gimbal._serverUrl, rB.volumeName).lookup(rB.path);
      if (!fileB.isDir()) throw new Error('expected b to be a directory');
    },
  },
];
