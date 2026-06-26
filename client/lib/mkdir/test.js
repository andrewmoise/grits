export const tests = [
  {
    label: 'mkdir creates a directory',
    async fn(shell, scratch) {
      await shell.eval(`gsh.path('${scratch}/newdir').mkdir()`);
      const r = shell.resolvePath(`${scratch}/newdir`);
      const file = await shell._vol(r.serverUrl, r.volume).lookup(r.path);
      if (!file.isDir()) throw new Error('expected a directory');
    },
  },
  {
    label: 'mkdir fails if path already exists',
    async fn(shell, scratch) {
      await shell.eval(`gsh.path('${scratch}/newdir').mkdir()`);
      let threw = false;
      try {
        await shell.eval(`gsh.path('${scratch}/newdir').mkdir()`);
      } catch (e) {
        if (e.message.includes('already exists')) threw = true;
        else throw e;
      }
      if (!threw) throw new Error('expected already-exists error');
    },
  },
  {
    label: 'mkdir with {f:1} succeeds if already exists',
    async fn(shell, scratch) {
      await shell.eval(`gsh.path('${scratch}/newdir').mkdir()`);
      await shell.eval(`gsh.path('${scratch}/newdir').mkdir({f:1})`);
      const r = shell.resolvePath(`${scratch}/newdir`);
      const file = await shell._vol(r.serverUrl, r.volume).lookup(r.path);
      if (!file.isDir()) throw new Error('expected a directory');
    },
  },
  {
    label: 'mkdir does not accept pipeline input',
    async fn(shell, scratch) {
      await shell.eval(`gsh.path('${scratch}/afile.txt').w('x')`);
      let threw = false;
      try {
        await shell.eval(`gsh.path('${scratch}/afile.txt').read().mkdir({p:1})`);
      } catch (e) {
        if (e.message.includes('need a path')) threw = true;
        else throw e;
      }
      if (!threw) throw new Error('expected path error');
    },
  },
  {
    label: 'mkdir requires a path argument',
    async fn(shell, scratch) {
      let threw = false;
      try {
        await shell.eval('gsh.mkdir()');
      } catch (e) {
        if (e.message.includes('need a path')) threw = true;
        else throw e;
      }
      if (!threw) throw new Error('expected usage error');
    },
  },
  {
    label: 'mkdir {p:1} creates intermediate directories',
    async fn(shell, scratch) {
      await shell.eval(`gsh.path('${scratch}/a/b/c').mkdir({p:1})`);
      const r = shell.resolvePath(`${scratch}/a/b/c`);
      const file = await shell._vol(r.serverUrl, r.volume).lookup(r.path);
      if (!file.isDir()) throw new Error('expected a directory');
    },
  },
  {
    label: 'mkdir {p:1} succeeds if directories already exist',
    async fn(shell, scratch) {
      await shell.eval(`gsh.path('${scratch}/a/b').mkdir({p:1})`);
      await shell.eval(`gsh.path('${scratch}/a/b').mkdir({p:1})`);
      const r = shell.resolvePath(`${scratch}/a/b`);
      const file = await shell._vol(r.serverUrl, r.volume).lookup(r.path);
      if (!file.isDir()) throw new Error('expected a directory');
    },
  },
  {
    label: 'mkdir {f:1,p:1} replaces file with directory',
    async fn(shell, scratch) {
      await shell.eval(`gsh.path('${scratch}/a').w('hi')`);
      await shell.eval(`gsh.path('${scratch}/a/b').mkdir({f:1,p:1})`);
      const rA = shell.resolvePath(`${scratch}/a`);
      const fileA = await shell._vol(rA.serverUrl, rA.volume).lookup(rA.path);
      if (!fileA.isDir()) throw new Error('expected a to be a directory');
      const rB = shell.resolvePath(`${scratch}/a/b`);
      const fileB = await shell._vol(rB.serverUrl, rB.volume).lookup(rB.path);
      if (!fileB.isDir()) throw new Error('expected b to be a directory');
    },
  },
];
