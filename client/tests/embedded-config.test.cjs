'use strict';
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const { test } = require('node:test');
const root = path.resolve(__dirname, '..');
const source = fs.readFileSync(path.join(root, 'config.embedded.js'), 'utf8')
  .replace('export default ', 'module.exports = ');

function config(context, origin = 'https://timeweaver.test:8443') {
  const sandbox = {
    module: { exports: {} }, window: { location: { origin } },
    document: { querySelector: () => context === null ? null : { getAttribute: () => context } },
  };
  vm.runInNewContext(source, sandbox);
  return sandbox.module.exports;
}

test('embedded API uses runtime context and the same origin', () => {
  for (const context of ['/time_weaver', '/timeweaver_svr', '/nested/api-v2']) {
    assert.equal(config(context).API_SERVER_URL, 'https://timeweaver.test:8443' + context);
  }
  assert.equal(config('/time_weaver', 'http://127.0.0.1:18080').API_SERVER_URL,
    'http://127.0.0.1:18080/time_weaver');
});

test('missing or unsafe runtime context fails instead of using port 8000', () => {
  for (const context of [null, '', '/', '//evil.invalid', '/tw/../other', '/tw?x', '/tw/']) {
    assert.throws(() => config(context), /API context/);
  }
});

function vueConfig(embedded, localExists) {
  const sandbox = {
    module: { exports: {} }, __dirname: root,
    process: { env: { TW_EMBEDDED_BUILD: embedded ? '1' : '' } },
    require: name => {
      if (name === '@vue/cli-service') return { defineConfig: value => value };
      if (name === 'fs') return { existsSync: () => localExists };
      if (name === 'path') return path;
      throw new Error('Unexpected module: ' + name);
    },
  };
  vm.runInNewContext(fs.readFileSync(path.join(root, 'vue.config.js'), 'utf8'), sandbox);
  return sandbox.module.exports;
}

test('embedded build ignores a developer local API address', () => {
  const actual = vueConfig(true, true);
  assert.equal(actual.configureWebpack.resolve.alias['@config'], path.join(root, 'config.embedded.js'));
  assert.equal(actual.outputDir, path.resolve(root, '../internal/webui/assets/dist'));
  assert.equal(actual.productionSourceMap, false);
});

test('existing standalone build keeps its local config and output', () => {
  assert.equal(vueConfig(false, true).configureWebpack.resolve.alias['@config'], path.join(root, 'config.js'));
  assert.equal(vueConfig(false, false).configureWebpack.resolve.alias['@config'], path.join(root, 'config.sample.js'));
  assert.equal(vueConfig(false, false).outputDir, 'dist');
});
