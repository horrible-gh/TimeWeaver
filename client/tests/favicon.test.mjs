import { test } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';

const publicDir = new URL('../public/', import.meta.url);
test('TimeWeaver clock favicon supplies real 16/32/48 pixel ICO entries', () => {
  const ico = fs.readFileSync(new URL('favicon.ico', publicDir));
  assert.equal(ico.readUInt16LE(0), 0);
  assert.equal(ico.readUInt16LE(2), 1);
  assert.equal(ico.readUInt16LE(4), 3);
  const sizes = [];
  for (let i = 0; i < 3; i++) {
    const entry = 6 + 16 * i;
    const width = ico[entry], height = ico[entry + 1];
    assert.equal(width, height);
    sizes.push(width);
    const length = ico.readUInt32LE(entry + 8), offset = ico.readUInt32LE(entry + 12);
    assert.ok(length > 100 && offset >= 54 && offset + length <= ico.length);
    assert.equal(ico.subarray(offset, offset + 8).toString('hex'), '89504e470d0a1a0a');
    assert.equal(ico.readUInt32BE(offset + 16), width);
    assert.equal(ico.readUInt32BE(offset + 20), height);
  }
  assert.deepEqual(sizes, [16, 32, 48]);
});
test('both Vue entry pages use the same root-relative branded favicon', () => {
  for (const page of ['login.html', 'dashboard.html']) {
    const html = fs.readFileSync(new URL(page, publicDir), 'utf8');
    assert.match(html, /<link rel="icon"[^>]+href="\/favicon\.ico\?v=tw-clock-1"/);
    assert.equal((html.match(/rel="icon"/g) || []).length, 1);
  }
});
