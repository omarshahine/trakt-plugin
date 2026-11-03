import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { promisify } from 'node:util';
import { mock, test } from 'node:test';

// Synthetic subprocess responses only: no local credentials or Trakt calls.
const calls = [];
const execFile = () => { throw new Error('unexpected callback invocation'); };
execFile[promisify.custom] = async (file, args) => {
	calls.push({ file, args });
	return { stdout: JSON.stringify({ items: [], page: '2', page_count: '3' }), stderr: '' };
};
mock.module('child_process', {
	namedExports: { execFile, execFileSync: () => '/synthetic/trakt-cli' },
});
mock.module('fs', { namedExports: { existsSync: () => true } });
mock.module('openclaw/plugin-sdk/plugin-entry', {
	namedExports: { definePluginEntry: (entry) => entry },
});
const { default: plugin } = await import('../dist/index.js');
const tools = [];
plugin.register({
	pluginConfig: { cliPath: '/synthetic/trakt-cli' },
	registerTool: (tool) => tools.push(tool),
});

test('custom list tools are registered and declared in the manifest', () => {
	const manifest = JSON.parse(readFileSync(new URL('../openclaw.plugin.json', import.meta.url)));
	for (const name of ['trakt_lists', 'trakt_list_items']) {
		assert.ok(tools.some((tool) => tool.name === name));
		assert.ok(manifest.contracts.tools.includes(name));
	}
	assert.ok(!tools.some((tool) => tool.name === 'trakt_sync'));
});

test('list discovery forwards page and limit and decodes JSON', async () => {
	const tool = tools.find((tool) => tool.name === 'trakt_lists');
	const result = await tool.execute('test', { page: 2, limit: 50 });
	assert.deepEqual(calls.at(-1), {
		file: '/synthetic/trakt-cli', args: ['lists', '--page', '2', '--limit', '50', '--json'],
	});
	assert.deepEqual(JSON.parse(result.content[0].text), { items: [], page: '2', page_count: '3' });
});

test('list items pass the slug as one argument, not shell text', async () => {
	const tool = tools.find((tool) => tool.name === 'trakt_list_items');
	const slug = 'synthetic; not-a-command';
	await tool.execute('test', { list: slug, page: 2, limit: 100 });
	assert.deepEqual(calls.at(-1).args, ['lists', 'items', '--list', slug, '--page', '2', '--limit', '100', '--json']);
	assert.deepEqual(tool.parameters.required, ['list']);
	assert.equal(tool.parameters.properties.page.minimum, 1);
	assert.equal(tool.parameters.properties.limit.maximum, 1000);
});

test('backup forwards a local path as one argument without invoking a shell', async () => {
	const tool = tools.find((tool) => tool.name === 'trakt_backup');
	const output = '/synthetic/private backups/new.zip';
	await tool.execute('test', { output, limit: 250, delay_ms: 0, max_pages: 10 });
	assert.deepEqual(calls.at(-1).args, ['backup', '--output', output, '--limit', '250', '--delay-ms', '0', '--max-pages', '10', '--json']);
	assert.deepEqual(tool.parameters.required, ['output']);
	const manifest = JSON.parse(readFileSync(new URL('../openclaw.plugin.json', import.meta.url)));
	assert.ok(manifest.contracts.tools.includes('trakt_backup'));
});
