import { GimbalClient } from '../gimbal/client.js';
import { GimbalResult } from '../gimbal/result.js';

export const help = `\
upload — open a file picker and bring the selected file into the pipeline

Usage:
  gimbal.upload()
  gimbal.upload({ accept: 'image/*' })
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

export function invoke(gimbal, prev, opts = {}) {
  if (!(prev instanceof GimbalClient)) throw new Error('upload: must be called on gimbal');
  return new GimbalResult(async () => {
    const file = await pickFile(opts.accept);
    return new Response(file, {
      headers: { 'Content-Type': file.type || 'application/octet-stream' },
    });
  });
}
