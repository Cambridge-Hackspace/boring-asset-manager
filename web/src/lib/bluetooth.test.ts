import { describe, expect, it } from 'vitest';
import { bleAddress, parseBLEAddress } from './bluetooth';

describe('Bluetooth LE addresses', () => {
  it('reads the address and what it overrides', () => {
    expect(parseBLEAddress('7C:2F:80:A1:B2:C3')).toEqual({ mac: '7C:2F:80:A1:B2:C3', service: '', write: '', notify: '', ack: '', chunk: '' });
    expect(parseBLEAddress('7C:2F:80:A1:B2:C3?service=0000fff0-0000-1000-8000-00805f9b34fb&write=fff2&ack=true')).toEqual({
      mac: '7C:2F:80:A1:B2:C3',
      service: '0000fff0-0000-1000-8000-00805f9b34fb',
      write: 'fff2',
      notify: '',
      ack: 'true',
      chunk: '',
    });
  });

  it('writes only what differs from the driver', () => {
    expect(bleAddress({ mac: ' 7C:2F:80:A1:B2:C3 ', service: '', write: '', notify: '', ack: '', chunk: '' })).toBe('7C:2F:80:A1:B2:C3');
    expect(bleAddress({ mac: '7C:2F:80:A1:B2:C3', service: 'fff0', write: ' fff2 ', notify: '', ack: 'false', chunk: '' })).toBe(
      '7C:2F:80:A1:B2:C3?service=fff0&write=fff2&ack=false',
    );
  });

  it('reads back what it writes', () => {
    const a = { mac: '7C:2F:80:A1:B2:C3', service: 'fff0', write: 'fff2', notify: 'fff1', ack: 'true', chunk: '' };
    expect(parseBLEAddress(bleAddress(a))).toEqual(a);
  });

  it('writes a browser connection’s, which has no Bluetooth address', () => {
    expect(bleAddress({ mac: '', service: '', write: '', notify: '', ack: '', chunk: '' })).toBe('');
    const a = { mac: '', service: 'fff0', write: 'fff2', notify: '', ack: '', chunk: '180' };
    expect(bleAddress(a)).toBe('?service=fff0&write=fff2&chunk=180');
    expect(parseBLEAddress(bleAddress(a))).toEqual(a);
  });
});
