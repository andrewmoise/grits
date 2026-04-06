// lib/rm/test.js
export const tests = [
  {
    label: 'rm removes a file',
    async fn(shell, scratch) {
      await shell.eval(`echo('hello').to('${scratch}/hello.txt')`);
      await shell.eval(`rm('${scratch}/hello.txt')`);
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
      await shell.eval(`mkdir('${scratch}/subdir')`);
      let threw = false;
      try {
        await shell.eval(`rm('${scratch}/subdir')`);
      } catch (e) {
        if (e.message.includes('is a directory')) threw = true;
        else throw e;
      }
      if (!threw) throw new Error('expected directory error');
    },
  },
  {
    label: 'rm with {f:1} removes a directory',
    async fn(shell, scratch) {
      await shell.eval(`mkdir('${scratch}/subdir')`);
      await shell.eval(`rm('${scratch}/subdir', {f:1})`);
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
        await shell.eval(`echo('hi').rm('${scratch}/hello.txt')`);
      } catch (e) {
        if (e.message.includes('pipeline input')) threw = true;
        else throw e;
      }
      if (!threw) throw new Error('expected pipeline input error');
    },
  },
  {
    label: 'rm requires a path argument',
    async fn(shell, scratch) {
      let threw = false;
      try {
        await shell.eval('rm()');
      } catch (e) {
        if (e.message.includes('expected rm(path)')) threw = true;
        else throw e;
      }
      if (!threw) throw new Error('expected usage error');
    },
  },
];