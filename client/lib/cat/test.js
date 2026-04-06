// lib/cat/test.js
export const tests = [
  {
    label: 'cat on a single path returns a Response',
    async fn(shell, scratch) {
      const value = await shell.eval("cat(':client/lib/echo/main.js')");
      if (!(value instanceof Response))
        throw new Error(`expected Response, got ${value?.constructor?.name}`);
    },
  },
  {
    label: 'cat on a single path has readable content',
    async fn(shell, scratch) {
      const value = await shell.eval("cat(':client/lib/echo/main.js')");
      const text = await value.text();
      if (!text.includes('invoke'))
        throw new Error('file content looks wrong');
    },
  },
  {
    label: 'cat wraps pipeline input as Response',
    async fn(shell, scratch) {
      const value = await shell.eval("echo('hello').cat()");
      if (!(value instanceof Response))
        throw new Error(`expected Response, got ${value?.constructor?.name}`);
      const text = await value.text();
      if (text !== 'hello')
        throw new Error(`expected 'hello', got '${text}'`);
    },
  },
  {
    label: 'cat concatenates multiple files',
    async fn(shell, scratch) {
      // Write two known files into scratch.
      await shell.eval(`echo('aaa').to('${scratch}/a.txt')`);
      await shell.eval(`echo('bbb').to('${scratch}/b.txt')`);
      const value = await shell.eval(`cat('${scratch}/a.txt', '${scratch}/b.txt')`);
      const text = await value.text();
      if (text !== 'aaabbb')
        throw new Error(`expected 'aaabbb', got '${text}'`);
    },
  },
  {
    label: 'cat prepends pipeline input to files',
    async fn(shell, scratch) {
      await shell.eval(`echo('bbb').to('${scratch}/b.txt')`);
      const value = await shell.eval(`echo('aaa').cat('${scratch}/b.txt')`);
      const text = await value.text();
      if (text !== 'aaabbb')
        throw new Error(`expected 'aaabbb', got '${text}'`);
    },
  },
  {
    label: 'cat with no argument and no pipeline input fails',
    async fn(shell, scratch) {
      let threw = false;
      try {
        await shell.eval('cat()');
      } catch (e) {
        if (e.message.includes('argument required')) threw = true;
        else throw e;
      }
      if (!threw) throw new Error('expected argument required error');
    },
  },
  {
    label: 'cat path arguments must be strings',
    async fn(shell, scratch) {
      let threw = false;
      try {
        await shell.eval('cat(42)');
      } catch (e) {
        if (e.message.includes('path arguments must be strings')) threw = true;
        else throw e;
      }
      if (!threw) throw new Error('expected type error');
    },
  },
];