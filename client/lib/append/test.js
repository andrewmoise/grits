// lib/append/test.js
export const tests = [
  {
    label: 'append adds content to the end of an existing file',
    async fn(shell, scratch) {
      await shell.eval(`echo('hello ').to('${scratch}/append.txt')`);
      await shell.eval(`echo('world').append('${scratch}/append.txt')`);
      const text = await shell.eval(`cat('${scratch}/append.txt').toText()`);
      if (text !== 'hello world')
        throw new Error(`expected 'hello world', got '${text}'`);
    },
  },
  {
    label: 'append creates file if it does not exist',
    async fn(shell, scratch) {
      await shell.eval(`echo('new file').append('${scratch}/new.txt')`);
      const text = await shell.eval(`cat('${scratch}/new.txt').toText()`);
      if (text !== 'new file')
        throw new Error(`expected 'new file', got '${text}'`);
    },
  },
  {
    label: 'append requires pipeline input',
    async fn(shell, scratch) {
      let threw = false;
      try {
        await shell.eval(`append('${scratch}/file.txt')`);
      } catch (e) {
        if (e.message.includes('requires pipeline input')) threw = true;
        else throw e;
      }
      if (!threw) throw new Error('expected pipeline input error');
    },
  },
  {
    label: 'append requires exactly one path argument',
    async fn(shell, scratch) {
      let threw = false;
      try {
        await shell.eval("echo('hi').append()");
      } catch (e) {
        if (e.message.includes('exactly one')) threw = true;
        else throw e;
      }
      if (!threw) throw new Error('expected path argument error');
    },
  },
  {
    label: 'append compounds across multiple calls',
    async fn(shell, scratch) {
      await shell.eval(`echo('a').to('${scratch}/compound.txt')`);
      await shell.eval(`echo('b').append('${scratch}/compound.txt')`);
      await shell.eval(`echo('c').append('${scratch}/compound.txt')`);
      const text = await shell.eval(`cat('${scratch}/compound.txt').toText()`);
      if (text !== 'abc')
        throw new Error(`expected 'abc', got '${text}'`);
    },
  },
  {
    label: 'append errors on directory destination',
    async fn(shell, scratch) {
      await shell.eval(`mkdir('${scratch}/append_dir')`);
      let threw = false;
      try {
        await shell.eval(`echo('data').append('${scratch}/append_dir')`);
      } catch (e) {
        if (e.message.includes('directory')) threw = true;
        else throw e;
      }
      if (!threw) throw new Error('expected directory error');
    },
  },
];
