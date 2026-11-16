import { loadEnvFile } from 'node:process';
import { spawn } from 'node:child_process';
try { loadEnvFile('../.env'); } catch (error) { if (error.code !== 'ENOENT') throw error; }
const port = process.env.FRONTEND_PORT ?? '3000';
if (!/^\d+$/.test(port) || Number(port) < 1 || Number(port) > 65535) throw new Error('Invalid FRONTEND_PORT');
const child = spawn(process.execPath, ['node_modules/next/dist/bin/next', process.argv[2], '--hostname', '127.0.0.1', '--port', port], { stdio: 'inherit', env: process.env });
for (const signal of ['SIGINT', 'SIGTERM']) process.on(signal, () => child.kill(signal));
child.on('exit', (code) => process.exit(code ?? 1));
