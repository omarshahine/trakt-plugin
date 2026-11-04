import assert from 'node:assert/strict';
import { promisify } from 'node:util';
import { beforeEach, mock, test } from 'node:test';

const calls = [];
const session = 'a'.repeat(32);
const pending = {
	success: true, status: 'pending', session,
	verification_url: 'https://trakt.tv/activate', user_code: 'SYNTHETIC-CODE',
	expires_in: 600, interval: 5,
};
let help = '  --session string  Poll an auth session';
let respond = async () => JSON.stringify(pending);
const execFile = () => { throw new Error('unexpected callback invocation'); };
execFile[promisify.custom] = async (cli, args, options) => {
	calls.push({ cli, args, options });
	return { stdout: args.includes('--help') ? help : await respond(args, options), stderr: '' };
};
mock.module('child_process', { namedExports: { execFile, execFileSync: () => '/synthetic/trakt-cli' } });
mock.module('fs', { namedExports: { existsSync: () => true } });
mock.module('openclaw/plugin-sdk/plugin-entry', { namedExports: { definePluginEntry: entry => entry } });
const { default: plugin } = await import('../dist/index.js');
function registered(config = {}) {
	const tools = [];
	plugin.register({ pluginConfig: { cliPath: '/synthetic/trakt-cli', ...config }, registerTool: tool => tools.push(tool) });
	return Object.fromEntries(tools.map(tool => [tool.name, tool]));
}
function response(result) { return JSON.parse(result.content[0].text); }
beforeEach(() => {
	calls.length = 0;
	help = '  --session string  Poll an auth session';
	respond = async () => JSON.stringify(pending);
});

test('all positional tool inputs follow supported flags and the option terminator', async () => {
	const tools = registered();
	const cases = [
		['trakt_search', { query: '--help', type: 'show' }, ['search', '--type', 'show', '--json', '--', '--help']],
		['trakt_history_add', { titles: ['--rewatch', 'Synthetic Show'], type: 'show', watched_at: '2026-01-01' }, ['history', 'add', '--type', 'show', '--watched-at', '2026-01-01', '--json', '--', '--rewatch', 'Synthetic Show']],
		['trakt_watchlist_add', { titles: ['--help'] }, ['watchlist', 'add', '--json', '--', '--help']],
		['trakt_watchlist_remove', { titles: ['--', '$(printf SYNTHETIC)'] }, ['watchlist', 'remove', '--json', '--', '--', '$(printf SYNTHETIC)']],
	];
	respond = async () => '{}';
	for (const [name, params, expected] of cases) {
		await tools[name].execute('test', params);
		assert.deepEqual(calls.at(-1).args, expected);
	}
});

test('closed handler validation rejects undeclared keys and invalid types before spawning', async () => {
	const tools = registered({ syncEnabled: true });
	const cases = [
		['trakt_sync', { dry_run: true, config: '/synthetic/executable.yaml' }],
		['trakt_sync', { dry_run: 'true' }],
		['trakt_history', { limit: '10' }],
		['trakt_search', { query: 1 }],
		['trakt_search', {}],
		['trakt_history_add', { titles: ['Synthetic', 12] }],
		['trakt_history_add', { titles: ['Synthetic'], rewatch: true }],
		['trakt_auth', { action: 'poll' }],
		['trakt_auth', { action: 'start', session }],
		['trakt_auth', { action: 'poll', session: '../not-a-session' }],
		['trakt_auth', { client_secret: 'SYNTHETIC' }],
	];
	for (const [name, params] of cases) {
		assert.equal(tools[name].parameters.additionalProperties, false);
		assert.equal(response(await tools[name].execute('test', params)).success, false);
	}
	assert.equal(calls.length, 0);
});

test('auth credentials use separate child environments and never argv or parent mutation', async t => {
	if (Object.hasOwn(process.env, 'TRAKT_CLIENT_ID') || Object.hasOwn(process.env, 'TRAKT_CLIENT_SECRET')) {
		t.skip('Requires a synthetic environment without inherited Trakt credentials');
		return;
	}
	process.env.TRAKT_CLIENT_ID = 'SYNTHETIC-PARENT-ID';
	process.env.TRAKT_CLIENT_SECRET = 'SYNTHETIC-PARENT-SECRET';
	try {
		const first = registered({ clientId: 'SYNTHETIC-ID-A', clientSecret: 'SYNTHETIC-SECRET-A' });
		const second = registered({ clientId: 'SYNTHETIC-ID-B', clientSecret: 'SYNTHETIC-SECRET-B' });
		await Promise.all([first.trakt_auth.execute('a', {}), second.trakt_auth.execute('b', {})]);
		const probes = calls.filter(call => call.args.includes('--help'));
		const starts = calls.filter(call => !call.args.includes('--help'));
		assert.equal(probes.length, 2);
		for (const call of probes) {
			assert.equal(Object.hasOwn(call.options.env, 'TRAKT_CLIENT_ID'), false);
			assert.equal(Object.hasOwn(call.options.env, 'TRAKT_CLIENT_SECRET'), false);
			assert.equal(call.options.timeout, 5000);
		}
		assert.equal(starts.length, 2);
		assert.deepEqual(starts.map(call => call.args), [['auth', '--json'], ['auth', '--json']]);
		assert.deepEqual(starts.map(call => call.options.env.TRAKT_CLIENT_ID).sort(), ['SYNTHETIC-ID-A', 'SYNTHETIC-ID-B']);
		assert.deepEqual(starts.map(call => call.options.env.TRAKT_CLIENT_SECRET).sort(), ['SYNTHETIC-SECRET-A', 'SYNTHETIC-SECRET-B']);
		assert.notEqual(starts[0].options.env, starts[1].options.env);
		assert.equal(process.env.TRAKT_CLIENT_ID, 'SYNTHETIC-PARENT-ID');
		assert.equal(process.env.TRAKT_CLIENT_SECRET, 'SYNTHETIC-PARENT-SECRET');
	} finally {
		delete process.env.TRAKT_CLIENT_ID;
		delete process.env.TRAKT_CLIENT_SECRET;
	}
});

test('old CLI is rejected before starting interactive auth and capability is checked again', async () => {
	const tool = registered().trakt_auth;
	help = 'Usage: trakt-cli auth\n  --client-secret string';
	assert.match(response(await tool.execute('old', {})).error, /Update trakt-cli/);
	assert.equal(calls.length, 1);
	assert.deepEqual(calls[0].args, ['auth', '--help']);
	help = '  --session string  Poll an auth session';
	assert.deepEqual(response(await tool.execute('new', {})), pending);
	assert.equal(calls.length, 3);
	help = 'Usage: trakt-cli auth';
	assert.match(response(await tool.execute('downgraded', {})).error, /Update trakt-cli/);
	assert.equal(calls.length, 4);
	assert.deepEqual(calls.at(-1).args, ['auth', '--help']);
});

test('auth start and poll return only the public nonblocking protocol', async () => {
	const tool = registered().trakt_auth;
	assert.deepEqual(response(await tool.execute('start', {})), pending);
	assert.deepEqual(calls.at(-1).args, ['auth', '--json']);
	const waiting = { success: true, status: 'pending', session, expires_in: 590, interval: 5 };
	respond = async () => JSON.stringify(waiting);
	assert.deepEqual(response(await tool.execute('waiting', { action: 'poll', session })), waiting);
	assert.deepEqual(calls.at(-1).args, ['auth', '--json', '--session', session]);
	respond = async () => JSON.stringify({ success: true, status: 'authenticated' });
	assert.deepEqual(response(await tool.execute('done', { action: 'poll', session })), { success: true, status: 'authenticated' });
});

test('unexpected auth stdout and secret-bearing protocol fields are never relayed', async () => {
	const secret = 'SYNTHETIC-PRIVATE-SECRET';
	const tool = registered({ clientSecret: secret }).trakt_auth;
	for (const value of [
		`raw credentials ${secret}`,
		JSON.stringify({ ...pending, access_token: 'SYNTHETIC-TOKEN' }),
		JSON.stringify({ ...pending, device_code: 'SYNTHETIC-DEVICE' }),
		JSON.stringify({ ...pending, user_code: secret }),
		JSON.stringify({ ...pending, user_code: secret }).replace('PRIVATE', '\\u0050RIVATE'),
		JSON.stringify({ ...pending, verification_url: 'http://trakt.tv/activate' }),
		JSON.stringify({ success: true, status: 'pending' }),
		JSON.stringify({ success: true, status: 'authenticated' }),
	]) {
		respond = async () => value;
		const text = (await tool.execute('test', {})).content[0].text;
		assert.equal(JSON.parse(text).success, false);
		assert.equal(text.includes(secret), false);
		assert.equal(text.includes('SYNTHETIC-TOKEN'), false);
		assert.equal(text.includes('SYNTHETIC-DEVICE'), false);
	}
	respond = async () => JSON.stringify(pending);
	assert.equal(response(await tool.execute('unexpected-instructions', { action: 'poll', session })).success, false);
	respond = async () => JSON.stringify({ ...pending, session: 'b'.repeat(32) });
	assert.equal(response(await tool.execute('mismatched', { action: 'poll', session })).success, false);
});

test('auth display fields accept only bounded codes and Trakt verification URLs', async () => {
	const secret = 'SYNTHETIC-PRIVATE-SECRET';
	const encodedSecret = [...secret].map(char => `%${char.charCodeAt(0).toString(16)}`).join('');
	const tool = registered({ clientSecret: secret }).trakt_auth;
	for (const verification_url of [
		'https://trakt.tv/activate',
		'https://www.trakt.tv/activate',
		'https://trakt.tv:443/activate',
		'https://www.trakt.tv:443/activate',
	]) {
		const valid = { ...pending, verification_url };
		respond = async () => JSON.stringify(valid);
		assert.deepEqual(response(await tool.execute('valid-url', {})), valid);
	}
	const invalidFields = [
		...[
			'https://example.invalid/activate',
			'https://trakt.tv.example.invalid/activate',
			'https://user:password@trakt.tv/activate',
			'https://trakt.tv:8443/activate',
			'https://trakt.tv/activate?hint=public',
			'https://trakt.tv/activate?',
			'https://trakt.tv/activate#fragment',
			'https://trakt.tv/activate#',
			`https://trakt.tv/activate?secret=${secret}`,
			`https://trakt.tv/activate?secret=${encodedSecret}`,
			`https://trakt.tv/${encodedSecret}`,
			'https://trakt.tv/activate\n',
		].map(verification_url => ({ verification_url })),
		...['', 'CODE WITH SPACE', 'CODE_WITH_UNDERSCORE', 'CODE\nCONTROL', 'CODE-\u263a', 'A'.repeat(129)]
			.map(user_code => ({ user_code })),
	];
	for (const fields of invalidFields) {
		respond = async () => JSON.stringify({ ...pending, ...fields });
		const result = response(await tool.execute('invalid-public-fields', {}));
		assert.equal(result.success, false);
		assert.equal(JSON.stringify(result).includes(secret), false);
		assert.equal(JSON.stringify(result).includes(encodedSecret), false);
	}
});


test('title-based bulk history deletion is absent from the agent tool surface', () => {
 for (const config of [{}, { syncEnabled: true }]) {
  const tools = registered(config);
  assert.equal(tools.trakt_history_remove, undefined);
  assert.ok(tools.trakt_history_remove_event);
 }
});
