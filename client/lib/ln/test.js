// lib/ln/test.js
export const tests = [
  {
    label: 'ln links a file to a new destination',
    async fn(shell, scratch) {
      await shell.eval(`echo('invoke test').to('${scratch}/main.js')`);
      await shell.eval(`ln('${scratch}/main.js', '${scratch}/echo.js')`);
      const text = await shell.eval(`cat('${scratch}/echo.js').toText()`);
      if (!text.includes('invoke'))
        throw new Error('linked file content looks wrong');
    },
  },
  {
    label: 'ln into a directory places file inside it',
    async fn(shell, scratch) {
      await shell.eval(`echo('invoke test').to('${scratch}/main.js')`);
      await shell.eval(`mkdir('${scratch}/subdir1')`);
      await shell.eval(`ln('${scratch}/main.js', '${scratch}/subdir1')`);
      const text = await shell.eval(`cat('${scratch}/subdir1/main.js').toText()`);
      if (!text.includes('invoke'))
        throw new Error('file not found inside directory');
    },
  },
  {
    label: 'ln with trailing slash fails if destination does not exist',
    async fn(shell, scratch) {
      let threw = false;
      try {
        await shell.eval(`echo('invoke test').to('${scratch}/main.js')`);
        await shell.eval(`ln('${scratch}/main.js', '${scratch}/notadir/')`);
      } catch (e) {
        threw = true;
      }
      if (!threw) throw new Error('expected an error');
    },
  },
  {
    label: 'ln with trailing slash and existing dir places file inside',
    async fn(shell, scratch) {
      await shell.eval(`echo('invoke test').to('${scratch}/main.js')`);
      await shell.eval(`mkdir('${scratch}/subdir2')`);
      await shell.eval(`ln('${scratch}/main.js', '${scratch}/subdir2/')`);
      const text = await shell.eval(`cat('${scratch}/subdir2/main.js').toText()`);
      if (!text.includes('invoke'))
        throw new Error('file not found inside directory');
    },
  },
  {
    label: 'ln overwrites an existing file by default',
    async fn(shell, scratch) {
      await shell.eval(`echo('invoke one').to('${scratch}/main.js')`);
      await shell.eval(`echo('invoke two').to('${scratch}/other.js')`);
      await shell.eval(`ln('${scratch}/main.js', '${scratch}/main.js')`);
      await shell.eval(`ln('${scratch}/other.js', '${scratch}/main.js')`);
      const text = await shell.eval(`cat('${scratch}/main.js').toText()`);
      if (!text.includes('invoke'))
        throw new Error('overwritten file content looks wrong');
    },
  },
  {
    label: 'ln into directory: first succeeds, second {i:1} fails, third overwrites',
    async fn(shell, scratch) {
      await shell.eval(`echo('invoke one').to('${scratch}/main.js')`);
      await shell.eval(`echo('invoke two').to('${scratch}/other.js')`);
      await shell.eval(`mkdir('${scratch}/subdir3')`);

      await shell.eval(`ln('${scratch}/main.js', '${scratch}/subdir3/', {i:1})`);
      const first = await shell.eval(`cat('${scratch}/subdir3/main.js').toText()`);
      if (!first.includes('invoke'))
        throw new Error('first link content looks wrong');

      let threw = false;
      try {
        await shell.eval(`ln('${scratch}/main.js', '${scratch}/subdir3/', {i:1})`);
      } catch (e) {
        if (e.message.includes('already exists')) threw = true;
        else throw e;
      }
      if (!threw) throw new Error('expected already-exists error on second link');

      await shell.eval(`ln('${scratch}/other.js', '${scratch}/subdir3/main.js')`);
      const third = await shell.eval(`cat('${scratch}/subdir3/main.js').toText()`);
      if (!third.includes('invoke'))
        throw new Error('third link content looks wrong');
      if (third === first)
        throw new Error('third link did not overwrite — content unchanged');
    },
  },
  {
    label: 'ln with {f:1} respects directory semantics',
    async fn(shell, scratch) {
      await shell.eval(`echo('invoke test').to('${scratch}/main.js')`);
      await shell.eval(`mkdir('${scratch}/subdir4')`);
      await shell.eval(`ln('${scratch}/main.js', '${scratch}/subdir4', {f:1})`);
      const text = await shell.eval(`cat('${scratch}/subdir4/main.js').toText()`);
      if (!text.includes('invoke'))
        throw new Error('file not placed inside directory');
    },
  },
  {
    label: 'ln with {ff:1} overwrites a directory',
    async fn(shell, scratch) {
      await shell.eval(`echo('invoke test').to('${scratch}/main.js')`);
      await shell.eval(`mkdir('${scratch}/subdir5')`);
      await shell.eval(`ln('${scratch}/main.js', '${scratch}/subdir5', {ff:1})`);
      const text = await shell.eval(`cat('${scratch}/subdir5').toText()`);
      if (!text.includes('invoke'))
        throw new Error('directory not overwritten by {ff:1}');
    },
  },
  {
    label: 'ln with {i:1} fails if destination exists',
    async fn(shell, scratch) {
      await shell.eval(`echo('invoke test').to('${scratch}/main.js')`);
      await shell.eval(`ln('${scratch}/main.js', '${scratch}/echo.js')`);
      let threw = false;
      try {
        await shell.eval(`ln('${scratch}/main.js', '${scratch}/echo.js', {i:1})`);
      } catch (e) {
        if (e.message.includes('already exists')) threw = true;
        else throw e;
      }
      if (!threw) throw new Error('expected already-exists error');
    },
  },
  {
    label: 'ln with {i:1} succeeds if destination does not exist',
    async fn(shell, scratch) {
      await shell.eval(`echo('invoke test').to('${scratch}/main.js')`);
      await shell.eval(`ln('${scratch}/main.js', '${scratch}/fresh.js', {i:1})`);
      const text = await shell.eval(`cat('${scratch}/fresh.js').toText()`);
      if (!text.includes('invoke'))
        throw new Error('file content looks wrong');
    },
  },
  {
    label: 'ln does not accept pipeline input',
    async fn(shell, scratch) {
      let threw = false;
      try {
        await shell.eval(`echo('invoke test').to('${scratch}/main.js')`);
        await shell.eval(`from('${scratch}/main.js').ln('${scratch}/out.js')`);
      } catch (e) {
        if (e.message.includes('pipeline input')) threw = true;
        else throw e;
      }
      if (!threw) throw new Error('expected pipeline input error');
    },
  },
  {
    label: 'ln with no arguments fails',
    async fn(shell, scratch) {
      let threw = false;
      try {
        await shell.eval('ln()');
      } catch (e) {
        if (e.message.includes('expected ln(src, dest)')) threw = true;
        else throw e;
      }
      if (!threw) throw new Error('expected usage error');
    },
  },
];
