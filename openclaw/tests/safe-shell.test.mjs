import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';
import ts from 'typescript';

// Exercise the real wrapper independently of plugin subprocess mocks.
const source = readFileSync(new URL('../src/safe-shell.ts', import.meta.url), 'utf8');
const { outputText } = ts.transpileModule(source, {
 compilerOptions: { module: ts.ModuleKind.ESNext, target: ts.ScriptTarget.ES2022 },
});
const { runCli } = await import(`data:text/javascript;base64,${Buffer.from(outputText).toString('base64')}`);

test('cooperatively finalized failures retain stdout and exit status', async () => {
 await assert.rejects(runCli(process.execPath, ['-e', `
  setTimeout(() => {
   console.log(JSON.stringify({ complete: false, errors: ['context deadline exceeded'] }));
   process.exitCode = 1;
  }, 10);
 `], { timeout: 1000, killSignal: 'SIGKILL' }), error => {
  assert.equal(error.code, 1);
  assert.equal(error.signal, null);
  assert.equal(JSON.parse(error.stdout).complete, false);
  return true;
 });
});

test('watchdog forcibly bounds a hung child that handles SIGTERM', async () => {
 await assert.rejects(runCli(process.execPath, ['-e', `
  process.on('SIGTERM', () => {});
  console.log('ready');
  setInterval(() => {}, 1000);
 `], { timeout: 1000, killSignal: 'SIGKILL' }), error => {
  assert.equal(error.killed, true);
  assert.equal(error.signal, 'SIGKILL');
  assert.equal(error.stdout.trim(), 'ready');
  return true;
 });
});
