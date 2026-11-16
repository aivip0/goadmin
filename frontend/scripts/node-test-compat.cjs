// This preload must be CommonJS for Node's --require hook.
// eslint-disable-next-line @typescript-eslint/no-require-imports
const util = require('node:util');
// eslint-disable-next-line @typescript-eslint/no-require-imports
const { syncBuiltinESMExports } = require('node:module');

if (typeof Map.groupBy !== 'function') {
  Object.defineProperty(Map, 'groupBy', {
    configurable: true,
    value(items, callback) {
      const groups = new Map();
      let index = 0;
      for (const item of items) {
        const key = callback(item, index++);
        const group = groups.get(key);
        if (group) group.push(item);
        else groups.set(key, [item]);
      }
      return groups;
    },
    writable: true,
  });
}

// Node 24.12 does not accept a format array; Vitest's bundler uses one.
const original = util.styleText;
util.styleText = (format, text, options) => Array.isArray(format)
  ? format.reduce((value, part) => original(part, value, options), text)
  : original(format, text, options);
syncBuiltinESMExports();
