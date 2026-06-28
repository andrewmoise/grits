export const tests = [
  {
    label: 'echo passes null through',
    async fn(gimbal, scratch) {
      const val = await gimbal.eval('gimbal.echo(null)');
      if (val !== null) throw new Error(`expected null, got ${JSON.stringify(val)}`);
    },
  },
  {
    label: 'echo passes a number through',
    async fn(gimbal, scratch) {
      const val = await gimbal.eval('gimbal.echo(42)');
      if (val !== 42) throw new Error(`expected 42, got ${val}`);
    },
  },
  {
    label: 'echo passes a string through',
    async fn(gimbal, scratch) {
      const val = await gimbal.eval('gimbal.echo("hello")');
      if (val !== 'hello') throw new Error(`expected 'hello', got ${JSON.stringify(val)}`);
    },
  },
  {
    label: 'echo passes an array through',
    async fn(gimbal, scratch) {
      const val = await gimbal.eval('gimbal.echo([1,2,3])');
      if (!Array.isArray(val)) throw new Error('expected array');
      if (val.length !== 3 || val[0] !== 1) throw new Error('unexpected content');
    },
  },
  {
    label: 'echo passes an object through',
    async fn(gimbal, scratch) {
      const val = await gimbal.eval('gimbal.echo({a:1,b:"two"})');
      if (typeof val !== 'object' || val === null) throw new Error('expected object');
      if (val.a !== 1 || val.b !== 'two') throw new Error('unexpected content');
    },
  },
  {
    label: 'echo throws on pipeline input',
    async fn(gimbal, scratch) {
      let threw = false;
      try {
        await gimbal.eval(`gimbal.p('${scratch}').echo(42)`);
      } catch (e) {
        if (e.message.includes('must be called on gimbal')) threw = true;
        else throw e;
      }
      if (!threw) throw new Error('expected pipeline input error');
    },
  },
];
