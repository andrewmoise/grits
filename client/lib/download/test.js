// lib/download/test.js
export const tests = [
  {
    label: 'download fetches a URL and returns a Response',
    async fn(shell, scratch) {
      const value = await shell.eval(
        `download('${shell.serverUrl}/grits/v1/content/client/lib/grits/GritsClient.js')`
      );
      if (!(value instanceof Response))
        throw new Error(`expected Response, got ${value?.constructor?.name}`);
      if (!value.ok)
        throw new Error(`expected ok response, got ${value.status}`);
    },
  },
  {
    label: 'download result matches same file fetched from volume',
    async fn(shell, scratch) {
      const resp = await shell.eval(
        `download('${shell.serverUrl}/grits/v1/content/client/lib/grits/GritsClient.js')`
      );
      const downloaded = await resp.text();

      const r = shell.resolvePath(':client/lib/grits/GritsClient.js');
      const file = await shell._vol(r.serverUrl, r.volume).lookup(r.path);
      const fromVol = await file.text();

      if (downloaded !== fromVol)
        throw new Error('downloaded content does not match volume content');
    },
  },
  {
    label: 'download does not accept pipeline input',
    async fn(shell, scratch) {
      let threw = false;
      try {
        await shell.eval(`echo('hi').download('${shell.serverUrl}/grits/v1/content/client/lib/grits/GritsClient.js')`);
      } catch (e) {
        if (e.message.includes('pipeline input')) threw = true;
        else throw e;
      }
      if (!threw) throw new Error('expected pipeline input error');
    },
  },
  {
    label: 'download requires a URL argument',
    async fn(shell, scratch) {
      let threw = false;
      try {
        await shell.eval('download()');
      } catch (e) {
        if (e.message.includes('URL string required')) threw = true;
        else throw e;
      }
      if (!threw) throw new Error('expected URL required error');
    },
  },
  {
    label: 'download requires a string URL, not other types',
    async fn(shell, scratch) {
      let threw = false;
      try {
        await shell.eval('download(42)');
      } catch (e) {
        if (e.message.includes('URL string required')) threw = true;
        else throw e;
      }
      if (!threw) throw new Error('expected URL string error');
    },
  },
];