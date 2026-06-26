export const tests = [
  {
    label: 'append adds content to the end of an existing file',
    async fn(shell, scratch) {
      await shell.eval(`gsh.p('${scratch}/append.txt').w('hello ')`);
      await shell.eval(`gsh.p('${scratch}/append.txt').append('world')`);
      const text = await shell.eval(`gsh.p('${scratch}/append.txt').read()`);
      if (text !== 'hello world')
        throw new Error(`expected 'hello world', got '${text}'`);
    },
  },
  {
    label: 'append creates file if it does not exist',
    async fn(shell, scratch) {
      await shell.eval(`gsh.p('${scratch}/new.txt').append('new file')`);
      const text = await shell.eval(`gsh.p('${scratch}/new.txt').read()`);
      if (text !== 'new file')
        throw new Error(`expected 'new file', got '${text}'`);
    },
  },
  {
    label: 'append requires content',
    async fn(shell, scratch) {
      let threw = false;
      try {
        await shell.eval(`gsh.p('${scratch}/f.txt').append()`);
      } catch (e) {
        if (e.message.includes('no content')) threw = true;
        else throw e;
      }
      if (!threw) throw new Error('expected content error');
    },
  },
  {
    label: 'append compounds across multiple calls',
    async fn(shell, scratch) {
      await shell.eval(`gsh.p('${scratch}/compound.txt').w('a')`);
      await shell.eval(`gsh.p('${scratch}/compound.txt').append('b')`);
      await shell.eval(`gsh.p('${scratch}/compound.txt').append('c')`);
      const text = await shell.eval(`gsh.p('${scratch}/compound.txt').read()`);
      if (text !== 'abc')
        throw new Error(`expected 'abc', got '${text}'`);
    },
  },
  {
    label: 'append errors on directory destination',
    async fn(shell, scratch) {
      await shell.eval(`gsh.p('${scratch}/append_dir').mkdir()`);
      let threw = false;
      try {
        await shell.eval(`gsh.p('${scratch}/append_dir').append('data')`);
      } catch (e) {
        if (e.message.includes('directory')) threw = true;
        else throw e;
      }
      if (!threw) throw new Error('expected directory error');
    },
  },
];
