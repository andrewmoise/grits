// lib/echo/test.js
export const tests = [
  {
    label: 'echo returns a Response',
    async fn(shell, scratch) {
      const value = await shell.eval("echo('hello world')");
      if (!(value instanceof Response))
        throw new Error(`expected Response, got ${value?.constructor?.name}`);
    },
  },
  {
    label: 'echo response contains the input string',
    async fn(shell, scratch) {
      const value = await shell.eval("echo('hello world')");
      const text = await value.text();
      if (text !== 'hello world')
        throw new Error(`expected 'hello world', got '${text}'`);
    },
  },
  {
    label: 'echo response has text/plain content type',
    async fn(shell, scratch) {
      const value = await shell.eval("echo('hello world')");
      if (!value.headers.get('Content-Type')?.includes('text/plain'))
        throw new Error('expected text/plain content type');
    },
  },
  {
    label: 'echo with no argument fails',
    async fn(shell, scratch) {
      let threw = false;
      try {
        await shell.eval('echo()');
      } catch (e) {
        if (e.message.includes('first argument must be a string')) threw = true;
        else throw e;
      }
      if (!threw) throw new Error('expected argument error');
    },
  },
  {
    label: 'echo coerces non-string argument to string',
    async fn(shell, scratch) {
      // echo uses String(text) so numbers should work
      const value = await shell.eval('echo(42).toText()');
      if (value !== '42')
        throw new Error(`expected '42', got '${value}'`);
    },
  },
];