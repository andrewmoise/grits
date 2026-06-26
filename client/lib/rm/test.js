export const tests = [
  {
    label: 'rm removes a file',
    async fn(shell, scratch) {
      await shell.eval(`gsh.path('${scratch}/hello.txt').w('hello')`);
      await shell.eval(`gsh.path('${scratch}/hello.txt').rm()`);
      let threw = false;
      try {
        const r = shell.resolvePath(`${scratch}/hello.txt`);
        await shell._vol(r.serverUrl, r.volume).lookup(r.path);
      } catch (e) {
        if (e.message.includes('not found')) threw = true;
        else throw e;
      }
      if (!threw) throw new Error('expected file to be gone');
    },
  },
  {
    label: 'rm fails on a directory by default',
    async fn(shell, scratch) {
      await shell.eval(`gsh.path('${scratch}/subdir').mkdir()`);
      let threw = false;
      try {
        await shell.eval(`gsh.path('${scratch}/subdir').rm()`);
      } catch (e) {
        if (e.message.includes('is a directory')) threw = true;
        else throw e;
      }
      if (!threw) throw new Error('expected directory error');
    },
  },
  {
    label: 'rm with {r:1} removes a directory',
    async fn(shell, scratch) {
      await shell.eval(`gsh.path('${scratch}/subdir').mkdir()`);
      await shell.eval(`gsh.path('${scratch}/subdir').rm({r:1})`);
      let threw = false;
      try {
        const r = shell.resolvePath(`${scratch}/subdir`);
        await shell._vol(r.serverUrl, r.volume).lookup(r.path);
      } catch (e) {
        if (e.message.includes('not found')) threw = true;
        else throw e;
      }
      if (!threw) throw new Error('expected directory to be gone');
    },
  },
  {
    label: 'rm does not accept pipeline input',
    async fn(shell, scratch) {
      let threw = false;
      try {
        await shell.eval(`gsh.path('${scratch}/f').w('x')`);
        await shell.eval(`gsh.path('${scratch}/f').read().rm()`);
      } catch (e) {
        if (e.message.includes('need a path')) threw = true;
        else throw e;
      }
      if (!threw) throw new Error('expected pipeline input error');
    },
  },
  {
    label: 'rm requires a path',
    async fn(shell, scratch) {
      let threw = false;
      try {
        await shell.eval('gsh.rm()');
      } catch (e) {
        if (e.message.includes('need a path')) threw = true;
        else throw e;
      }
      if (!threw) throw new Error('expected usage error');
    },
  },
  {
    label: 'rm rejects non-string arguments',
    async fn(shell, scratch) {
      let threw = false;
      try {
        await shell.eval('gsh.rm(42)');
      } catch (e) {
        if (e.message.includes('need a path')) threw = true;
        else throw e;
      }
      if (!threw) throw new Error('expected usage error for non-string arg');
    },
  },
  {
    label: 'rm fails on non-existent path by default',
    async fn(shell, scratch) {
      let threw = false;
      try {
        await shell.eval(`gsh.path('${scratch}/nope.txt').rm()`);
      } catch (e) {
        if (e.message.includes('not found')) threw = true;
        else throw e;
      }
      if (!threw) throw new Error('expected not found error');
    },
  },
  {
    label: 'rm with {f:1} ignores non-existent path',
    async fn(shell, scratch) {
      await shell.eval(`gsh.path('${scratch}/nope.txt').rm({f:1})`);
    },
  },
  {
    label: 'rm with {f:1} still fails on directory',
    async fn(shell, scratch) {
      await shell.eval(`gsh.path('${scratch}/dir').mkdir()`);
      let threw = false;
      try {
        await shell.eval(`gsh.path('${scratch}/dir').rm({f:1})`);
      } catch (e) {
        if (e.message.includes('is a directory')) threw = true;
        else throw e;
      }
      if (!threw) throw new Error('expected directory error with {f:1}');
    },
  },
];
