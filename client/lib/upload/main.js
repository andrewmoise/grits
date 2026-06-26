import { GimbalResult } from '../gimbal/result.js';
import { GimbalShell } from '../gimbal/gsh.js';

export const help = `\
upload — open a file picker and bring the selected file into the pipeline

Usage:
  gsh.upload()
  gsh.upload({ accept: 'image/*' })
  upload().to(path)     — pipe to .w() instead .w()

Output is a Response wrapping the selected file's contents.`;

function pickFile(accept) {
  return new Promise((resolve, reject) => {
    const input = document.createElement('input');
    input.type = 'file';
    if (accept) input.accept = accept;
    input.addEventListener('change', () => {
      const file = input.files?.[0];
      if (file) resolve(file);
      else reject(new Error('upload: no file selected'));
    });
    input.click();
  });
}

export function invoke(prev, opts = {}) {
  if (!(prev instanceof GimbalShell)) throw new Error('upload: must be called on gsh');

  return new GimbalResult(async () => {
    const file = await pickFile(opts.accept);
    return new Response(file, {
      headers: { 'Content-Type': file.type || 'application/octet-stream' },
    });
  });
}
