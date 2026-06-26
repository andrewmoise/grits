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
    label: 'echo response contains the input string with trailing newline',
    async fn(shell, scratch) {
      const value = await shell.eval("echo('hello world')");
      const text = await value.text();
      if (text !== 'hello world\n')
        throw new Error(`expected 'hello world\\n', got '${text}'`);
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
      const value = await shell.eval('echo(42).toText()');
      if (value !== '42\n')
        throw new Error(`expected '42\\n', got '${value}'`);
    },
  },
  {
    label: 'echo {n:1} suppresses trailing newline',
    async fn(shell, scratch) {
      const value = await shell.eval("echo('hello', {n:1})");
      const text = await value.text();
      if (text !== 'hello')
        throw new Error(`expected 'hello', got '${text}'`);
    },
  },
  {
    label: 'echo {j:1} JSON-encodes the string with trailing newline',
    async fn(shell, scratch) {
      const value = await shell.eval("echo('hello world', {j:1})");
      const text = await value.text();
      if (text !== '"hello world"\n')
        throw new Error(`expected '"hello world"\\n', got '${text}'`);
    },
  },
  {
    label: 'echo {j:1} escapes special characters',
    async fn(shell, scratch) {
      const value = await shell.eval("echo('line with \"quotes\"', {j:1})");
      const text = await value.text();
      if (text !== '"line with \\"quotes\\""\n')
        throw new Error(`expected JSON-escaped string, got '${text}'`);
    },
  },
  {
    label: 'echo {j:1, n:1} JSON-encodes without trailing newline',
    async fn(shell, scratch) {
      const value = await shell.eval("echo('hello', {j:1, n:1})");
      const text = await value.text();
      if (text !== '"hello"')
        throw new Error(`expected '"hello"', got '${text}'`);
    },
  },
];