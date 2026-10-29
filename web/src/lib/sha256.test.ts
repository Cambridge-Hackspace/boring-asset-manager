import { describe, expect, it } from 'vitest';
import { createHash } from 'node:crypto';
import { Sha256 } from './sha256';

const hex = (s: string | Uint8Array) => new Sha256().update(typeof s === 'string' ? new TextEncoder().encode(s) : s).hex();

describe('Sha256', () => {
  it('gives the standard digests', () => {
    expect(hex('')).toBe('e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855');
    expect(hex('abc')).toBe('ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad');
    expect(hex('abcdbcdecdefdefgefghfghighijhijkijkljklmklmnlmnomnopnopq')).toBe(
      '248d6a61d20638b8e5c026930c3e6039a33ce45964ff2167f6ecedd419db06c1',
    );
  });

  it('gives the same digest however the bytes are fed', () => {
    const data = new Uint8Array(200_003);
    for (let i = 0; i < data.length; i++) data[i] = (i * 31 + (i >> 7)) & 0xff;
    const want = createHash('sha256').update(data).digest('hex');
    expect(hex(data)).toBe(want);
    for (const step of [1, 3, 55, 56, 63, 64, 65, 4096, 100_000]) {
      const h = new Sha256();
      for (let i = 0; i < data.length; i += step) h.update(data.subarray(i, i + step));
      expect(h.hex(), `in steps of ${step}`).toBe(want);
    }
  });

  it('pads lengths near a block', () => {
    for (let n = 50; n < 135; n++) {
      const data = new Uint8Array(n).fill(0x61);
      expect(hex(data)).toBe(createHash('sha256').update(data).digest('hex'));
    }
  });
});
