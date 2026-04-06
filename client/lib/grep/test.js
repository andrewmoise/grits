// lib/grep/test.js
export const tests = [
  {
    label: 'grep filters lines from pipeline input',
    async fn(shell, scratch) {
      const value = await shell.eval("echo('foo\\nbar\\nbaz').grep('ba')");
      if (typeof value !== 'string')
        throw new Error(`expected string, got ${value?.constructor?.name}`);
      if (!value.includes('bar') || !value.includes('baz') || value.includes('foo'))
        throw new Error(`unexpected grep output: ${value}`);
    },
  },
  {
    label: 'grep with {invert:true} returns non-matching lines',
    async fn(shell, scratch) {
      const value = await shell.eval("echo('foo\\nbar\\nbaz').grep('ba', {invert:true})");
      if (!value.includes('foo') || value.includes('bar') || value.includes('baz'))
        throw new Error(`unexpected inverted grep output: ${value}`);
    },
  },
  {
    label: 'grep with {ignoreCase:true} matches case-insensitively',
    async fn(shell, scratch) {
      const value = await shell.eval("echo('Foo\\nbar\\nFOO').grep('foo', {ignoreCase:true})");
      if (!value.includes('Foo') || !value.includes('FOO') || value.includes('bar'))
        throw new Error(`unexpected case-insensitive output: ${value}`);
    },
  },
  {
    label: 'grep with {fixed:true} treats pattern as literal string',
    async fn(shell, scratch) {
      const value = await shell.eval("echo('foo.bar\\nfooXbar').grep('foo.bar', {fixed:true})");
      if (!value.includes('foo.bar') || value.includes('fooXbar'))
        throw new Error(`unexpected fixed grep output: ${value}`);
    },
  },
  {
    label: 'grep on a path argument filters that file',
    async fn(shell, scratch) {
      const value = await shell.eval("grep('invoke', ':client/lib/echo/main.js')");
      if (typeof value !== 'string')
        throw new Error(`expected string, got ${value?.constructor?.name}`);
      if (!value.includes('invoke'))
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
        await shell.eval("echo('foo').grep('foo', ':client/lib/echo/main.js')");
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