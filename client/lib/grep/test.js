// lib/grep/test.js
export const tests = [
  {
    label: 'grep filters lines from pipeline input',
    async fn(shell, scratch) {
      const value = await shell.eval("echo('foo\\nbar\\nbaz').grep('ba')");
      if (!(value instanceof Response))
        throw new Error(`expected Response, got ${value?.constructor?.name}`);
      const text = await value.text();
      if (!text.includes('bar') || !text.includes('baz') || text.includes('foo'))
        throw new Error(`unexpected grep output: ${text}`);
    },
  },
  {
    label: 'grep with {invert:true} returns non-matching lines',
    async fn(shell, scratch) {
      const text = await shell.eval("echo('foo\\nbar\\nbaz').grep('ba', {invert:true}).toText()");
      if (!text.includes('foo') || text.includes('bar') || text.includes('baz'))
        throw new Error(`unexpected inverted grep output: ${text}`);
    },
  },
  {
    label: 'grep with {ignoreCase:true} matches case-insensitively',
    async fn(shell, scratch) {
      const text = await shell.eval("echo('Foo\\nbar\\nFOO').grep('foo', {ignoreCase:true}).toText()");
      if (!text.includes('Foo') || !text.includes('FOO') || text.includes('bar'))
        throw new Error(`unexpected case-insensitive output: ${text}`);
    },
  },
  {
    label: 'grep with {fixed:true} treats pattern as literal string',
    async fn(shell, scratch) {
      const text = await shell.eval("echo('foo.bar\\nfooXbar').grep('foo.bar', {fixed:true}).toText()");
      if (!text.includes('foo.bar') || text.includes('fooXbar'))
        throw new Error(`unexpected fixed grep output: ${text}`);
    },
  },
  {
    label: 'grep on a path argument filters that file',
    async fn(shell, scratch) {
      await shell.eval(`echo('hello\\ninvoke\\nworld').to('${scratch}/file.txt')`);
      const text = await shell.eval(`grep('invoke', '${scratch}/file.txt').toText()`);
      if (!text.includes('invoke'))
        throw new Error('expected to find "invoke" in grep output');
    },
  },
  {
    label: 'grep requires a pattern',
    async fn(shell, scratch) {
      let threw = false;
      try {
        await shell.eval("echo('foo').grep()");
      } catch (e) {
        if (e.message.includes('pattern required')) threw = true;
        else throw e;
      }
      if (!threw) throw new Error('expected pattern required error');
    },
  },
  {
    label: 'grep cannot combine pipeline input with path arguments',
    async fn(shell, scratch) {
      let threw = false;
      try {
        await shell.eval(`echo('foo').to('${scratch}/file.txt')`);
        await shell.eval(`echo('foo').grep('foo', '${scratch}/file.txt')`);
      } catch (e) {
        if (e.message.includes('cannot combine')) threw = true;
        else throw e;
      }
      if (!threw) throw new Error('expected combine error');
    },
  },
  {
    label: 'grep requires either pipeline input or path arguments',
    async fn(shell, scratch) {
      let threw = false;
      try {
        await shell.eval("grep('foo')");
      } catch (e) {
        if (e.message.includes('requires either')) threw = true;
        else throw e;
      }
      if (!threw) throw new Error('expected requires either error');
    },
  },
];
