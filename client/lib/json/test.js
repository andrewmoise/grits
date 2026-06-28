export const tests = [
  {
    label: 'json serializes a string read from a file',
    async fn(gimbal, scratch) {
      await gimbal.eval(`gimbal.p('${scratch}/f.txt').w('hello')`);
      const json = await gimbal.eval(`gimbal.p('${scratch}/f.txt').read().json()`);
      if (json !== '"hello"')
        throw new Error(`expected '"hello"', got ${JSON.stringify(json)}`);
    },
  },
  {
    label: 'json serializes an array from ls',
    async fn(gimbal, scratch) {
      await gimbal.eval(`gimbal.p('${scratch}/a.txt').w('a')`);
      await gimbal.eval(`gimbal.p('${scratch}/b.txt').w('b')`);
      const json = await gimbal.eval(`gimbal.p('${scratch}').ls().json()`);
      const parsed = JSON.parse(json);
      if (!Array.isArray(parsed)) throw new Error('expected array');
      if (parsed.length !== 2) throw new Error(`expected 2 entries, got ${parsed.length}`);
    },
  },
  {
    label: 'json handles null',
    async fn(gimbal, scratch) {
      const json = await gimbal.eval('gimbal.echo(null).json()');
      if (json !== 'null')
        throw new Error(`expected 'null', got ${JSON.stringify(json)}`);
    },
  },
  {
    label: 'json serializes an arbitrary object via echo',
    async fn(gimbal, scratch) {
      const json = await gimbal.eval('gimbal.echo({a:1,b:"two"}).json()');
      const parsed = JSON.parse(json);
      if (parsed.a !== 1 || parsed.b !== 'two')
        throw new Error(`unexpected content: ${json}`);
    },
  },
];
