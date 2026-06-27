import { GimbalResult } from '../gimbal/result.js';

export const help = `\
download — fetch a URL and bring the response into the pipeline

Usage:
  gimbal.download('https://example.com/data.json')
  gimbal.download('https://example.com/data.json', { headers: { Authorization: '...' } })

Output is a Response. Pipe to .w() to save to a file.`;

export function invoke(gimbal, prev, url, init) {
  if (!url || typeof url !== 'string') throw new Error('download: URL string required');

  return new GimbalResult(async () => {
    let response;
    try {
      response = await fetch(url, init);
    } catch (e) {
      throw new Error(`download: network error fetching ${url}: ${e.message}`);
    }
    return response;
  });
}
