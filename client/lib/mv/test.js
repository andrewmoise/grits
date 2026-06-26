export const tests = [
  {
    label: 'mv moves a file',
    async fn(shell, scratch) {
      await shell.eval(`gsh.path('${scratch}/src.txt').w('hello')`);
      await shell.eval(`gsh.path('${scratch}/src.txt').mv(gsh.path('${scratch}/dest.txt'))`);
      const text = await shell.eval(`gsh.path('${scratch}/dest.txt').read()`);
      if (text !== 'hello') throw new Error(`expected 'hello' at dest, got '${text}'`);
      let threw = false;
      try {
        const r = shell.resolvePath(`${scratch}/src.txt`);
        await shell._vol(r.serverUrl, r.volume).lookup(r.path);
      } catch (e) {
        if (e.message.includes('not found')) threw = true;
        else throw e;
      }
      if (!threw) throw new Error('expected source to be gone');
    },
  },
  {
    label: 'mv fails if destination exists',
    async fn(shell, scratch) {
      await shell.eval(`gsh.path('${scratch}/src.txt').w('hello')`);
      await shell.eval(`gsh.path('${scratch}/dest.txt').w('world')`);
      let threw = false;
      try {
        await shell.eval(`gsh.path('${scratch}/src.txt').mv(gsh.path('${scratch}/dest.txt'))`);
      } catch (e) {
        if (e.message.includes('destination exists')) threw = true;
        else throw e;
      }
      if (!threw) throw new Error('expected already-exists error');
    },
  },
  {
    label: 'mv with {f:1} overwrites destination',
    async fn(shell, scratch) {
      await shell.eval(`gsh.path('${scratch}/src.txt').w('hello')`);
      await shell.eval(`gsh.path('${scratch}/dest.txt').w('world')`);
      await shell.eval(`gsh.path('${scratch}/src.txt').mv(gsh.path('${scratch}/dest.txt'), {f:1})`);
      const text = await shell.eval(`gsh.path('${scratch}/dest.txt').read()`);
      if (text !== 'hello') throw new Error(`expected 'hello' at dest, got '${text}'`);
    },
  },
  {
    label: 'mv does not accept pipeline input',
    async fn(shell, scratch) {
      await shell.eval(`gsh.path('${scratch}/src.txt').w('x')`);
      let threw = false;
      try {
        await shell.eval(`gsh.path('${scratch}/src.txt').read().mv(gsh.path('${scratch}/dest.txt'))`);
      } catch (e) {
        if (e.message.includes('need a source path')) threw = true;
        else throw e;
      }
      if (!threw) throw new Error('expected pipeline input error');
    },
  },
  {
    label: 'mv requires two path arguments',
    async fn(shell, scratch) {
      let threw = false;
      try {
        await shell.eval('gsh.mv()');
      } catch (e) {
        if (e.message.includes('need a source path')) threw = true;
        else throw e;
      }
      if (!threw) throw new Error('expected usage error');
    },
  },
  {
    label: 'mv into directory places file inside it',
    async fn(shell, scratch) {
      await shell.eval(`gsh.path('${scratch}/dir1').mkdir()`);
      await shell.eval(`gsh.path('${scratch}/src.txt').w('hello')`);
      await shell.eval(`gsh.path('${scratch}/src.txt').mv(gsh.path('${scratch}/dir1'))`);
      const text = await shell.eval(`gsh.path('${scratch}/dir1/src.txt').read()`);
      if (text !== 'hello') throw new Error('file not moved into directory');
    },
  },
  {
    label: 'mv with {f:1} respects directory semantics',
    async fn(shell, scratch) {
      await shell.eval(`gsh.path('${scratch}/dir2').mkdir()`);
      await shell.eval(`gsh.path('${scratch}/src.txt').w('hello')`);
      await shell.eval(`gsh.path('${scratch}/src.txt').mv(gsh.path('${scratch}/dir2'), {f:1})`);
      const text = await shell.eval(`gsh.path('${scratch}/dir2/src.txt').read()`);
      if (text !== 'hello') throw new Error('file not moved into directory');
    },
  },
  {
    label: 'mv with {ff:1} overwrites directory',
    async fn(shell, scratch) {
      await shell.eval(`gsh.path('${scratch}/dir3').mkdir()`);
      await shell.eval(`gsh.path('${scratch}/src.txt').w('hello')`);
      await shell.eval(`gsh.path('${scratch}/src.txt').mv(gsh.path('${scratch}/dir3'), {ff:1})`);
      const text = await shell.eval(`gsh.path('${scratch}/dir3').read()`);
      if (text !== 'hello') throw new Error('directory not overwritten');
    },
  },
  {
    label: 'mv with trailing slash (normalized, behaves as file)',
    async fn(shell, scratch) {
      await shell.eval(`gsh.path('${scratch}/src.txt').w('hello')`);
      await shell.eval(`gsh.path('${scratch}/src.txt').mv(gsh.path('${scratch}/dest'), {f:1})`);
      const text = await shell.eval(`gsh.path('${scratch}/dest').read()`);
      if (text !== 'hello') throw new Error('mv failed');
    },
  },
];
