export const tests = [
  {
    label: 'rmdir removes an empty directory',
    async fn(shell, scratch) {
      await shell.eval(`gsh.path('${scratch}/emptydir').mkdir()`);
      await shell.eval(`gsh.path('${scratch}/emptydir').rmdir()`);
      let threw = false;
      try {
        const r = shell.resolvePath(`${scratch}/emptydir`);
        await shell._vol(r.serverUrl, r.volume).lookup(r.path);
      } catch (e) {
        if (e.message.includes('not found')) threw = true;
        else throw e;
      }
      if (!threw) throw new Error('expected directory to be gone');
    },
  },
  {
    label: 'rmdir removes a non-empty directory',
    async fn(shell, scratch) {
      await shell.eval(`gsh.path('${scratch}/fulldir').mkdir()`);
      await shell.eval(`gsh.path('${scratch}/fulldir/file.txt').w('hi')`);
      await shell.eval(`gsh.path('${scratch}/fulldir').rmdir()`);
      let threw = false;
      try {
        const r = shell.resolvePath(`${scratch}/fulldir`);
        await shell._vol(r.serverUrl, r.volume).lookup(r.path);
      } catch (e) {
        if (e.message.includes('not found')) threw = true;
        else throw e;
      }
      if (!threw) throw new Error('expected directory to be gone');
    },
  },
  {
    label: 'rmdir fails on a file',
    async fn(shell, scratch) {
      await shell.eval(`gsh.path('${scratch}/file.txt').w('hi')`);
      let threw = false;
      try {
        await shell.eval(`gsh.path('${scratch}/file.txt').rmdir()`);
      } catch (e) {
        if (e.message.includes('not a directory')) threw = true;
        else throw e;
      }
      if (!threw) throw new Error('expected not-a-directory error');
    },
  },
  {
    label: 'rmdir does not accept pipeline input',
    async fn(shell, scratch) {
      let threw = false;
      try {
        await shell.eval(`gsh.path('${scratch}/d').mkdir()`);
        await shell.eval(`gsh.path('${scratch}/d').read().rmdir()`);
      } catch (e) {
        if (e.message.includes('need a path')) threw = true;
        else throw e;
      }
      if (!threw) throw new Error('expected error');
    },
  },
  {
    label: 'rmdir requires a path argument',
    async fn(shell, scratch) {
      let threw = false;
      try {
        await shell.eval('gsh.rmdir()');
      } catch (e) {
        if (e.message.includes('need a path')) threw = true;
        else throw e;
      }
      if (!threw) throw new Error('expected usage error');
    },
  },
];
