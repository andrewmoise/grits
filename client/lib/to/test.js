// lib/to/test.js
export const tests = [
  {
    label: 'to writes pipeline text to a path',
    async fn(shell, scratch) {
      await shell.eval(`echo('hello world').to('${scratch}/hello.txt')`);
      const text = await shell.eval(`cat('${scratch}/hello.txt').toText()`);
      if (text !== 'hello world')
        throw new Error(`expected 'hello world', got '${text}'`);
    },
  },
  {
    label: 'to overwrites an existing file by default',
    async fn(shell, scratch) {
      await shell.eval(`echo('first').to('${scratch}/file.txt')`);
      await shell.eval(`echo('second').to('${scratch}/file.txt')`);
      const text = await shell.eval(`cat('${scratch}/file.txt').toText()`);
      if (text !== 'second')
        throw new Error(`expected 'second', got '${text}'`);
    },
  },
  {
    label: 'to does not remap into a directory — uses path as-is',
    async fn(shell, scratch) {
      await shell.eval(`mkdir('${scratch}/to_subdir_1')`);
      // to() should fail here since subdir is a directory and {f:1} not set
      let threw = false;
      try {
        await shell.eval(`echo('hello').to('${scratch}/to_subdir_1')`);
      } catch (e) {
        if (e.message.includes('destination is a directory')) threw = true;
        else throw e;
      }
      if (!threw) throw new Error('expected directory error');
    },
  },
  {
    label: 'to with {f:1} overwrites a directory',
    async fn(shell, scratch) {
      await shell.eval(`mkdir('${scratch}/to_subdir_1')`);
      await shell.eval(`echo('hello').to('${scratch}/to_subdir_1', {f:1})`);
      const text = await shell.eval(`cat('${scratch}/to_subdir_1').toText()`);
      if (text !== 'hello')
        throw new Error(`expected 'hello', got '${text}'`);
    },
  },
  {
    label: 'to with {i:1} fails if destination exists',
    async fn(shell, scratch) {
      await shell.eval(`echo('first').to('${scratch}/file.txt')`);
      let threw = false;
      try {
        await shell.eval(`echo('second').to('${scratch}/file.txt', {i:1})`);
      } catch (e) {
        if (e.message.includes('already exists')) threw = true;
        else throw e;
      }
      if (!threw) throw new Error('expected already-exists error');
    },
  },
  {
    label: 'to with {i:1} succeeds if destination does not exist',
    async fn(shell, scratch) {
      await shell.eval(`echo('hello').to('${scratch}/fresh.txt', {i:1})`);
      const text = await shell.eval(`cat('${scratch}/fresh.txt').toText()`);
      if (text !== 'hello')
        throw new Error(`expected 'hello', got '${text}'`);
    },
  },
  {
    label: 'to requires pipeline input',
    async fn(shell, scratch) {
      let threw = false;
      try {
        await shell.eval(`to('${scratch}/file.txt')`);
      } catch (e) {
        if (e.message.includes('requires pipeline input')) threw = true;
        else throw e;
      }
      if (!threw) throw new Error('expected pipeline input error');
    },
  },
  {
    label: 'to requires exactly one path argument',
    async fn(shell, scratch) {
      let threw = false;
      try {
        await shell.eval("echo('hi').to()");
      } catch (e) {
        if (e.message.includes('exactly one destination')) threw = true;
        else throw e;
      }
      if (!threw) throw new Error('expected path argument error');
    },
  },
];