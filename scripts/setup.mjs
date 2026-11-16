import { readFileSync, writeFileSync } from 'node:fs';
import { randomBytes } from 'node:crypto';
const example = readFileSync(new URL('../.env.example', import.meta.url), 'utf8');
const secrets = {
  POSTGRES_PASSWORD: randomBytes(24).toString('hex'),
  CSRF_SECRET: randomBytes(32).toString('hex'),
  SETTINGS_ENCRYPTION_KEY: randomBytes(32).toString('base64'),
  PLUGIN_CONFIG_ENCRYPTION_KEY: randomBytes(32).toString('base64'),
};
const generated = example.split('\n').map(line => {
  const key = line.split('=', 1)[0];
  return Object.hasOwn(secrets, key) ? `${key}=${secrets[key]}` : line;
}).join('\n');
try {
  writeFileSync(new URL('../.env', import.meta.url), generated, { flag: 'wx', mode: 0o600 });
  console.log('Created root .env with independent database, CSRF and encryption secrets.');
} catch (error) {
  if (error.code !== 'EEXIST') throw error;
  console.log('Existing .env preserved.');
}
