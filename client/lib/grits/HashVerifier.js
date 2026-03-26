// HashVerifier.js — Content integrity verification for Grits blobs
//
// Verifies that fetched content matches its expected CID (SHA-256 multihash).
// Designed to sit between the network layer and the consumer so that
// mirror responses are never trusted blindly.
//
// Usage:
//   const verifier = new HashVerifier({ debug: false });
//   const result   = await verifier.verify(response, expectedCID);
//   // result: { ok: true, response } | { ok: false, error: string }

const BASE58_ALPHABET = '123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz';

function _base58Encode(buffer) {
  let zeros = 0;
  while (zeros < buffer.length && buffer[zeros] === 0) zeros++;
  const size = Math.floor((buffer.length - zeros) * 138 / 100) + 1;
  const b58  = new Uint8Array(size);
  let length = 0;
  for (let i = zeros; i < buffer.length; i++) {
    let carry = buffer[i], j = 0;
    for (let k = b58.length - 1; k >= 0; k--, j++) {
      if (carry === 0 && j >= length) break;
      carry += 256 * b58[k];
      b58[k] = carry % 58;
      carry  = Math.floor(carry / 58);
    }
    length = j;
  }
  let i = b58.length - length;
  while (i < b58.length && b58[i] === 0) i++;
  let str = '1'.repeat(zeros);
  for (; i < b58.length; i++) str += BASE58_ALPHABET[b58[i]];
  return str;
}

async function _computeCID(buffer) {
  const digest    = new Uint8Array(await crypto.subtle.digest('SHA-256', buffer));
  const multihash = new Uint8Array(34);
  multihash[0] = 0x12; // SHA-256 code
  multihash[1] = 0x20; // 32-byte digest length
  multihash.set(digest, 2);
  return _base58Encode(multihash);
}

export default class HashVerifier {
  constructor({ debug = false } = {}) {
    this._debug = debug;
  }

  /**
   * Verify that a Response body matches the expected CID.
   *
   * Consumes the *clone* internally; the returned response is safe to read.
   *
   * @param {Response} response - The fetch Response to verify (will be cloned)
   * @param {string}   expectedCID - The expected Base58 multihash
   * @returns {Promise<{ ok: boolean, response?: Response, error?: string }>}
   */
  async verify(response, expectedCID) {
    try {
      const verifyClone = response.clone();
      const buffer      = await verifyClone.arrayBuffer();
      const startTime   = this._debug ? performance.now() : 0;

      const actualCID = await _computeCID(buffer);

      if (this._debug) {
        const elapsed = (performance.now() - startTime).toFixed(2);
        console.log(`[HashVerifier] ${expectedCID.substring(0, 8)}: verified in ${elapsed}ms`);
      }

      if (actualCID === expectedCID) {
        return { ok: true, response };
      }

      const error = `Hash mismatch — expected ${expectedCID}, got ${actualCID}`;
      console.error(`[HashVerifier] ${error}`);
      return { ok: false, error };

    } catch (err) {
      const error = `Verification error: ${err.message}`;
      console.error(`[HashVerifier] ${error}`);
      return { ok: false, error };
    }
  }

  /**
   * Convenience: verify and return a Response, or a 502 on failure.
   * Drop-in for the pattern used in the original GritsClient.
   *
   * @param {Response} response
   * @param {string}   expectedCID
   * @returns {Promise<Response>}
   */
  async verifyOrReject(response, expectedCID) {
    const result = await this.verify(response, expectedCID);
    if (result.ok) return result.response;

    return new Response(
      `Hash verification failed: ${result.error}`,
      { status: 502, headers: { 'Content-Type': 'text/plain' } }
    );
  }
}
