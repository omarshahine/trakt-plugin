import assert from 'node:assert/strict';
import { promisify } from 'node:util';
import { mock, test } from 'node:test';

const secret = 'synthetic-secret with spaces';
const clientId = 'synthetic-client-id';
let failure;
const execFile = () => { throw new Error('unexpected callback invocation'); };
execFile[promisify.custom] = async () => { throw failure; };
mock.module('child_process', { namedExports: { execFile, execFileSync: () => '/synthetic/trakt-cli' } });
mock.module('fs', { namedExports: { existsSync: () => true } });
mock.module('openclaw/plugin-sdk/plugin-entry', { namedExports: { definePluginEntry: (entry) => entry } });
const { default: plugin } = await import('../dist/index.js');
const tools = [];
plugin.register({
	pluginConfig: { cliPath: '/synthetic/trakt-cli', clientId, clientSecret: secret },
	registerTool: (tool) => tools.push(tool),
});

test('authentication failures never expose command lines, stdout or stderr', async () => {
	for (const error of [
		Object.assign(new Error(`Command failed: trakt-cli auth --client-id ${clientId} --client-secret ${secret}`), {
			stderr: `request rejected: ${secret}`, stdout: `payload contains ${clientId} ${secret}`,
		}),
		Object.assign(new Error(`spawn failed: ${secret}`), { code: 'ENOENT' }),
		{ stderr: secret, stdout: secret, message: secret },
		secret,
	]) {
		failure = error;
		const result = await tools.find((tool) => tool.name === 'trakt_auth').execute('test', {});
		const text = result.content[0].text;
		assert.ok(!text.includes(secret));
		assert.ok(!text.includes(clientId));
		assert.ok(!text.includes('--client-secret'));
		assert.equal(JSON.parse(text).success, false);
		assert.match(JSON.parse(text).error, /authentication failed/);
	}
});

test('non-auth rate limit diagnostics still work', async () => {
	failure = new Error('rate limited by trakt api: 429 (path=/users/me/history retry_after=17)');
	const result = await tools.find((tool) => tool.name === 'trakt_history').execute('test', {});
	const response = JSON.parse(result.content[0].text);
	assert.equal(response.rate_limited, true);
	assert.equal(response.retry_after_seconds, 17);
});
