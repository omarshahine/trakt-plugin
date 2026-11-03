import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { promisify } from 'node:util';
import { mock, test } from 'node:test';

// Synthetic subprocess responses only: no local credentials or Trakt calls.
const calls = [];
let mutationFailure;
const execFile = () => { throw new Error('unexpected callback invocation'); };
execFile[promisify.custom] = async (file, args) => {
	calls.push({ file, args });
	if (mutationFailure) throw mutationFailure;
	return { stdout: JSON.stringify({ items: [], page: '2', page_count: '3' }), stderr: '' };
};
mock.module('child_process', {
	namedExports: { execFile, execFileSync: () => '/synthetic/trakt-cli' },
});
let authConfigured = true;
mock.module('fs', { namedExports: { existsSync: () => authConfigured } });
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

test('favorites restore exposes membership-only offline review and explicit apply parameters',async()=>{
	const tool=tools.find(t=>t.name==='trakt_backup_restore_favorites');const manifest=JSON.parse(readFileSync(new URL('../openclaw.plugin.json',import.meta.url)));assert.ok(tool && manifest.contracts.tools.includes(tool.name));assert.deepEqual(tool.parameters.required,['source','current']);
	const params={source:'/synthetic/favorites source.zip',current:'/synthetic/favorites;current.zip',limit:2};authConfigured=false;
	try{await tool.execute('test',params);assert.deepEqual(calls.at(-1).args,['backup','restore-favorites','--source',params.source,'--current',params.current,'--limit','2','--json']);const count=calls.length;const denied=await tool.execute('test',{...params,apply:true});assert.equal(calls.length,count);assert.equal(JSON.parse(denied.content[0].text).success,false);}finally{authConfigured=true;}
	await tool.execute('test',{...params,review_id:'b'.repeat(64),expect_user:'synthetic',apply:true});assert.ok(calls.at(-1).args.includes('--apply'));assert.ok(!calls.at(-1).args.includes('--rank'));assert.ok(!calls.at(-1).args.includes('--notes'));
});

test('favorites restore partial and rate-limit states stay visible without mutation replay',async()=>{
	const tool=tools.find(t=>t.name==='trakt_backup_restore_favorites');const review='b'.repeat(64);
	for(const throttled of [false,true]){
		const count=calls.length;mutationFailure=Object.assign(new Error('synthetic favorites failure'),{stdout:JSON.stringify({review_id:review,outcome:throttled?'unknown':'partial',attempted:true,verified:false,rows:[{type:'movie',trakt_id:'55',state:'present_verified'},{type:'show',trakt_id:'56',state:'missing'}]}),stderr:throttled?'rate limited by trakt api: 429 (path=/sync/favorites retry_after=60)':'partial membership; inspect before retry'});
		try{const result=await tool.execute('test',{source:'/synthetic/source.zip',current:'/synthetic/current.zip',review_id:review,expect_user:'synthetic',apply:true});const parsed=JSON.parse(result.content[0].text);assert.equal(calls.length,count+1);assert.equal(parsed.operation.attempted,true);assert.equal(parsed.operation.rows[1].state,'missing');if(throttled){assert.equal(parsed.retry_after_seconds,60);assert.equal(parsed.rate_limited,true);}}finally{mutationFailure=undefined;}
	}
});

test('ratings restore preview is offline, bounded and forwards reviewed apply parameters literally', async () => {
	const tool=tools.find(t=>t.name==='trakt_backup_restore_ratings');
	const manifest=JSON.parse(readFileSync(new URL('../openclaw.plugin.json',import.meta.url)));
	assert.ok(tool && manifest.contracts.tools.includes(tool.name));
	assert.deepEqual(tool.parameters.required,['source','current']);
	assert.equal(tool.parameters.properties.limit.maximum,100);
	const params={source:'/synthetic/desired archive.zip',current:'/synthetic/current;literal.zip',limit:2};
	authConfigured=false;
	try{
		await tool.execute('test',params);
		assert.deepEqual(calls.at(-1).args,['backup','restore-ratings','--source',params.source,'--current',params.current,'--limit','2','--json']);
		const count=calls.length;const denied=await tool.execute('test',{...params,apply:true});assert.equal(calls.length,count);assert.equal(JSON.parse(denied.content[0].text).success,false);
	}finally{authConfigured=true;}
	await tool.execute('test',{...params,review_id:'a'.repeat(64),expect_user:'synthetic',apply:true});
	assert.deepEqual(calls.at(-1).args,['backup','restore-ratings','--source',params.source,'--current',params.current,'--limit','2','--review-id','a'.repeat(64),'--expect-user','synthetic','--apply','--json']);
});

test('partial rating restores retain reviewed row outcomes without replay',async()=>{
	const tool=tools.find(t=>t.name==='trakt_backup_restore_ratings');const review='a'.repeat(64);const count=calls.length;
	mutationFailure=Object.assign(new Error('synthetic partial rating restore'),{stdout:JSON.stringify({review_id:review,outcome:'partial',attempted:true,verified:false,rows:[{type:'movie',trakt_id:'55',state:'present_verified'},{type:'episode',trakt_id:'56',state:'missing'}]}),stderr:'partial result; inspect ratings before retrying'});
	try{const result=await tool.execute('test',{source:'/synthetic/source.zip',current:'/synthetic/current.zip',review_id:review,expect_user:'synthetic',apply:true});const parsed=JSON.parse(result.content[0].text);assert.equal(calls.length,count+1);assert.equal(parsed.operation.outcome,'partial');assert.equal(parsed.operation.rows[1].state,'missing');}finally{mutationFailure=undefined;}
});

test('single history-event removal previews offline and keeps precise IDs as literal arguments', async () => {
	const tool = tools.find(t => t.name === 'trakt_history_remove_event');
	const manifest = JSON.parse(readFileSync(new URL('../openclaw.plugin.json', import.meta.url)));
	assert.ok(tool && manifest.contracts.tools.includes(tool.name));
	assert.deepEqual(tool.parameters.required, ['history_id', 'type', 'trakt_id', 'watched_at']);
	const params = { history_id: '9007199254740993', type: 'episode', trakt_id: '55', watched_at: '2020-01-01T00:00:00.123456789Z', expect_user: 'synthetic;literal' };
	authConfigured = false;
	try {
		await tool.execute('test', params);
		assert.deepEqual(calls.at(-1).args, ['history', 'remove-event', '--history-id', params.history_id, '--type', 'episode', '--trakt-id', '55', '--watched-at', params.watched_at, '--expect-user', params.expect_user, '--json']);
		const count = calls.length;
		const denied = await tool.execute('test', { ...params, apply: true });
		assert.equal(calls.length, count);
		assert.equal(JSON.parse(denied.content[0].text).success, false);
	} finally { authConfigured = true; }
	await tool.execute('test', { ...params, apply: true });
	assert.ok(calls.at(-1).args.includes('--apply'));
});

test('uncertain history-event removal stays visible and is never replayed', async () => {
	const tool = tools.find(t => t.name === 'trakt_history_remove_event');
	mutationFailure = Object.assign(new Error('synthetic readback failure'), { stdout: JSON.stringify({ target: { history_id: '9007199254740993' }, outcome: 'unknown', attempted: true, verified: false }), stderr: 'readback failed; do not automatically retry' });
	const count = calls.length;
	try {
		const result = await tool.execute('test', { history_id: '9007199254740993', type: 'movie', trakt_id: '55', watched_at: '2020-01-01T00:00:00Z', expect_user: 'synthetic', apply: true });
		assert.equal(calls.length, count + 1);
		assert.match(result.content[0].text, /unknown/);
		assert.match(result.content[0].text, /attempted/);
	} finally { mutationFailure = undefined; }
});

test('history-event rate limits retain whether a deletion was attempted', async () => {
	const tool = tools.find(t => t.name === 'trakt_history_remove_event');
	mutationFailure = Object.assign(new Error('synthetic rate limit'), { stdout: JSON.stringify({ target: { history_id: '9007199254740993' }, outcome: 'unknown', attempted: true, verified: false }), stderr: 'rate limited by trakt api: 429 (path=/sync/history/remove retry_after=60)' });
	try {
		const result = await tool.execute('test', { history_id: '9007199254740993', type: 'movie', trakt_id: '55', watched_at: '2020-01-01T00:00:00Z', expect_user: 'synthetic', apply: true });
		const parsed = JSON.parse(result.content[0].text);
		assert.equal(parsed.rate_limited, true);
		assert.equal(parsed.retry_after_seconds, 60);
		assert.equal(parsed.operation.attempted, true);
		assert.equal(parsed.operation.verified, false);
	} finally { mutationFailure = undefined; }
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
	await tool.execute('test', { output, limit: 250, delay_ms: 0, max_pages: 10, website_supplement: '/synthetic/private supplements/account.json' });
	assert.deepEqual(calls.at(-1).args, ['backup', '--output', output, '--limit', '250', '--delay-ms', '0', '--max-pages', '10', '--website-supplement', '/synthetic/private supplements/account.json', '--json']);
	assert.deepEqual(tool.parameters.required, ['output']);
	const manifest = JSON.parse(readFileSync(new URL('../openclaw.plugin.json', import.meta.url)));
	assert.ok(manifest.contracts.tools.includes('trakt_backup'));
});

test('restore planner works offline without auth and only forwards review-file arguments', async () => {
	const tool = tools.find((tool) => tool.name === 'trakt_backup_plan_restore');
	const manifest = JSON.parse(readFileSync(new URL('../openclaw.plugin.json', import.meta.url)));
	assert.ok(manifest.contracts.tools.includes(tool.name));
	authConfigured = false;
	try {
		await tool.execute('test', { source: '/synthetic/source.zip', current: '/synthetic/current.zip', import_json: '/synthetic/review; literal.json' });
		assert.deepEqual(calls.at(-1).args, ['backup', 'plan-restore', '--source', '/synthetic/source.zip', '--current', '/synthetic/current.zip', '--import-json', '/synthetic/review; literal.json', '--json']);
		assert.deepEqual(tool.parameters.required, ['source', 'current']);
	} finally { authConfigured = true; }
});

test('local backup comparison works without auth and passes paths as literal arguments', async () => {
	const tool = tools.find((tool) => tool.name === 'trakt_backup_compare');
	const manifest = JSON.parse(readFileSync(new URL('../openclaw.plugin.json', import.meta.url)));
	assert.ok(manifest.contracts.tools.includes('trakt_backup_compare'));
	const before = '/synthetic/private backups/before.zip';
	const after = '/synthetic/after; not-a-command.zip';
	const previousCalls = calls.length;
	authConfigured = false;
	try {
		await tool.execute('test', { before, after, sample_limit: 0 });
		assert.equal(calls.length, previousCalls + 1);
		assert.deepEqual(calls.at(-1).args, ['backup', 'compare', '--before', before, '--after', after, '--sample-limit', '0', '--json']);
	} finally {
		authConfigured = true;
	}
});

test('all list writes preview offline and require explicit apply to pass the gate', async () => {
	const manifest = JSON.parse(readFileSync(new URL('../openclaw.plugin.json', import.meta.url)));
	const cases = [
		['create', { name: 'Synthetic; literal', privacy: 'private' }, ['--name', 'Synthetic; literal', '--privacy', 'private']],
		['add', { list: '7', type: 'movie', ids: ['9007199254740993', '55'] }, ['--list', '7', '--type', 'movie', '--ids', '9007199254740993,55']],
		['remove', { list: '7', type: 'episode', ids: ['55'] }, ['--list', '7', '--type', 'episode', '--ids', '55']],
		['notes', { list: '7', item: '900', notes: '' }, ['--list', '7', '--item', '900', '--notes', '']],
		['reorder', { list: '7', rank: ['901', '900'] }, ['--list', '7', '--rank', '901,900']],
	];
	authConfigured = false;
	try {
		for (const [action, params, expected] of cases) {
			const tool = tools.find(t => t.name === `trakt_list_${action}`);
			assert.ok(tool && manifest.contracts.tools.includes(tool.name));
			await tool.execute('test', params);
			assert.deepEqual(calls.at(-1).args, ['lists', action, ...expected, '--json']);
			const count = calls.length;
			const denied = await tool.execute('test', { ...params, apply: true });
			assert.equal(calls.length, count);
			assert.equal(JSON.parse(denied.content[0].text).success, false);
		}
	} finally { authConfigured = true; }
	const tool = tools.find(t => t.name === 'trakt_list_remove');
	await tool.execute('test', { list: '7', type: 'movie', ids: ['55'], apply: true });
	assert.deepEqual(calls.at(-1).args, ['lists', 'remove', '--list', '7', '--type', 'movie', '--ids', '55', '--apply', '--json']);
});

test('partial list writes preserve structured results and are not retried', async () => {
	const tool = tools.find(t => t.name === 'trakt_list_add');
	mutationFailure = Object.assign(new Error('synthetic partial membership result'), {
		stdout: JSON.stringify({ action: 'add', outcome: 'partial', http_status: 201, response: { added: { movies: 1 }, not_found: { movies: [{ ids: { trakt: 56 } }] } } }),
		stderr: 'some items were not found; inspect result before retrying',
	});
	const count = calls.length;
	try {
		const result = await tool.execute('test', { list: '7', type: 'movie', ids: ['55', '56'], apply: true });
		const parsed = JSON.parse(result.content[0].text);
		assert.equal(parsed.success, false);
		assert.equal(parsed.operation.outcome, 'partial');
		assert.equal(parsed.operation.response.added.movies, 1);
		assert.equal(calls.length, count + 1);
	} finally { mutationFailure = undefined; }
});

test('ratings/favorites register typed reads and offline preview-first writes', async () => {
	const manifest = JSON.parse(readFileSync(new URL('../openclaw.plugin.json', import.meta.url)));
	for (const resource of ['ratings', 'favorites']) {
		const tool = tools.find(t => t.name === `trakt_${resource}`);
		assert.ok(tool && manifest.contracts.tools.includes(tool.name));
		await tool.execute('test', { type: 'movie', page: 2, limit: 50 });
		assert.deepEqual(calls.at(-1).args, [resource, '--type', 'movie', '--page', '2', '--limit', '50', '--json']);
	}
	const cases = [
		['trakt_rating_set', 'ratings', 'set', { type: 'episode', ids: ['9007199254740993'], rating: 9, rated_at: '2026-01-01T00:00:00Z' }, ['--type', 'episode', '--ids', '9007199254740993', '--rating', '9', '--rated-at', '2026-01-01T00:00:00Z']],
		['trakt_rating_remove', 'ratings', 'remove', { type: 'season', ids: ['55'] }, ['--type', 'season', '--ids', '55']],
		['trakt_favorite_add', 'favorites', 'add', { type: 'movie', ids: ['55', '56'] }, ['--type', 'movie', '--ids', '55,56']],
		['trakt_favorite_remove', 'favorites', 'remove', { type: 'show', ids: ['55'] }, ['--type', 'show', '--ids', '55']],
	];
	authConfigured = false;
	try {
		for (const [name, resource, action, params, expected] of cases) {
			const tool = tools.find(t => t.name === name);
			assert.ok(tool && manifest.contracts.tools.includes(name));
			await tool.execute('test', params);
			assert.deepEqual(calls.at(-1).args, [resource, action, ...expected, '--json']);
			const count = calls.length;
			const denied = await tool.execute('test', { ...params, apply: true, expect_user: 'test-account' });
			assert.equal(calls.length, count);
			assert.equal(JSON.parse(denied.content[0].text).success, false);
		}
	} finally { authConfigured = true; }
	const tool = tools.find(t => t.name === 'trakt_rating_set');
	assert.ok(tool.parameters.required.includes('rating'));
	assert.equal(tool.parameters.properties.rating.minimum, 1);
	assert.equal(tool.parameters.properties.rating.maximum, 10);
	await tool.execute('test', { type: 'movie', ids: ['55'], rating: 8, apply: true, expect_user: 'test-account' });
	assert.deepEqual(calls.at(-1).args, ['ratings', 'set', '--type', 'movie', '--ids', '55', '--rating', '8', '--apply', '--expect-user', 'test-account', '--json']);
});

test('preference partial results stay visible and are not replayed', async () => {
	const tool = tools.find(t => t.name === 'trakt_favorite_add');
	mutationFailure = Object.assign(new Error('synthetic partial result'), {
		stdout: JSON.stringify({ resource: 'favorites', action: 'add', outcome: 'partial', http_status: 201, response: { added: { movies: 1 }, not_found: { movies: [{ ids: { trakt: 56 } }] } } }),
		stderr: 'some preference items were not found; inspect result before retrying',
	});
	const count = calls.length;
	try {
		const result = await tool.execute('test', { type: 'movie', ids: ['55', '56'], apply: true, expect_user: 'test-account' });
		const parsed = JSON.parse(result.content[0].text);
		assert.equal(parsed.success, false);
		assert.equal(parsed.operation.resource, 'favorites');
		assert.equal(parsed.operation.outcome, 'partial');
		assert.equal(calls.length, count + 1);
	} finally { mutationFailure = undefined; }
});

 test('collection planner is offline, registered and has no apply schema',async()=>{
 const tool=tools.find(t=>t.name==='trakt_backup_plan_collection');const manifest=JSON.parse(readFileSync(new URL('../openclaw.plugin.json',import.meta.url)));assert.ok(tool && manifest.contracts.tools.includes(tool.name));assert.deepEqual(tool.parameters.required,['source','current']);assert.ok(!('apply' in tool.parameters.properties));authConfigured=false;
 try{await tool.execute('collection-test',{source:'/synthetic/source.zip',current:'/synthetic/current.zip'});assert.deepEqual(calls.at(-1).args,['backup','plan-collection','--source','/synthetic/source.zip','--current','/synthetic/current.zip','--json']);}finally{authConfigured=true;}
 });
