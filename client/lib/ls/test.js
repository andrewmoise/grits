export const tests = [
  {
    label: 'ls lists current directory as array',
    async fn(shell, scratch) {
      const list = await shell.eval(`gsh.p('${scratch}').ls()`);
      if (!Array.isArray(list)) throw new Error(`expected array, got ${typeof list}`);
    },
  },
  {
    label: 'ls shows files that exist in directory',
    async fn(shell, scratch) {
      await shell.eval(`gsh.p('${scratch}/hello.txt').w('hello')`);
      const list = await shell.eval(`gsh.p('${scratch}').ls()`);
      if (!list.some(p => p.toString() === 'hello.txt'))
        throw new Error(`expected hello.txt in listing, got ${JSON.stringify(list)}`);
    },
  },
  {
    label: 'ls returns sorted results',
    async fn(shell, scratch) {
      await shell.eval(`gsh.p('${scratch}/c.txt').w('a')`);
      await shell.eval(`gsh.p('${scratch}/a.txt').w('b')`);
      await shell.eval(`gsh.p('${scratch}/b.txt').w('c')`);
      const list = await shell.eval(`gsh.p('${scratch}').ls()`);
      const names = list.map(p => p.toString());
      if (JSON.stringify(names) !== JSON.stringify([...names].sort()))
        throw new Error(`expected sorted list, got ${JSON.stringify(names)}`);
    },
  },
  {
    label: 'ls on a file fails',
    async fn(shell, scratch) {
      await shell.eval(`gsh.p('${scratch}/hello.txt').w('hello')`);
      let threw = false;
      try {
        await shell.eval(`gsh.p('${scratch}/hello.txt').ls()`);
      } catch (e) {
        if (e.message.includes('not a directory')) threw = true;
        else throw e;
      }
      if (!threw) throw new Error('expected not-a-directory error');
    },
  },
];
