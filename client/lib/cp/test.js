// lib/cp/test.js
export const tests = [
  {
    label: 'cp copies a file',
    async fn(shell, scratch) {
      await shell.eval(`echo('hello').to('${scratch}/src.txt')`);
      await shell.eval(`cp('${scratch}/src.txt', '${scratch}/dest.txt')`);
      const text = await shell.eval(`cat('${scratch}/dest.txt').toText()`);
      if (text !== 'hello') throw new Error('copy failed');
    },
  },
  {
    label: 'cp into directory places file inside',
    async fn(shell, scratch) {
      await shell.eval(`mkdir('${scratch}/dir1')`);
      await shell.eval(`echo('hello').to('${scratch}/src.txt')`);
      await shell.eval(`cp('${scratch}/src.txt', '${scratch}/dir1')`);
      const text = await shell.eval(`cat('${scratch}/dir1/src.txt').toText()`);
      if (text !== 'hello') throw new Error('not copied into directory');
    },
  },
  {
    label: 'cp with trailing slash requires directory',
    async fn(shell, scratch) {
      await shell.eval(`echo('hello').to('${scratch}/src.txt')`);
      let threw = false;
      try {
        await shell.eval(`cp('${scratch}/src.txt', '${scratch}/notadir/')`);
      } catch (e) {
        if (e.message.includes('not a directory')) threw = true;
        else throw e;
      }
      if (!threw) throw new Error('expected not-a-directory error');
    },
  },
  {
    label: 'cp with {f:1} respects directory semantics',
    async fn(shell, scratch) {
      await shell.eval(`mkdir('${scratch}/dir2')`);
      await shell.eval(`echo('hello').to('${scratch}/src.txt')`);
      await shell.eval(`cp('${scratch}/src.txt', '${scratch}/dir2', {f:1})`);
      const text = await shell.eval(`cat('${scratch}/dir2/src.txt').toText()`);
      if (text !== 'hello') throw new Error('not copied into directory');
    },
  },
  {
    label: 'cp with {ff:1} overwrites directory',
    async fn(shell, scratch) {
      await shell.eval(`mkdir('${scratch}/dir3')`);
      await shell.eval(`echo('hello').to('${scratch}/src.txt')`);
      await shell.eval(`cp('${scratch}/src.txt', '${scratch}/dir3', {ff:1})`);
      const text = await shell.eval(`cat('${scratch}/dir3').toText()`);
      if (text !== 'hello') throw new Error('directory not overwritten');
    },
  },
];
