/**
 * OpenClaw plugin entry for trakt-cli.
 *
 * Registers tools that shell out to the `trakt-cli` binary.
 * Each tool maps to a CLI subcommand (search, history, watchlist, progress).
 */

import { definePluginEntry } from 'openclaw/plugin-sdk/plugin-entry';
import { Type, type TSchema } from '@sinclair/typebox';
import { existsSync } from 'fs';
import { homedir } from 'os';
import { join } from 'path';
import { runCli, whichBinary } from './safe-shell.js';

interface PluginConfig {
	cliPath?: string;
	clientId?: string;
	clientSecret?: string;
	/** Opt-in: register trakt_sync. Off by default so existing installs are unchanged. */
	syncEnabled?: boolean;
}

interface ToolDef {
	name: string;
	command: string;
	subcommand?: string;
	description: string;
	parameters: ReturnType<typeof Type.Object>;
}

// Opt-in streaming sync tool. Registered only when pluginConfig.syncEnabled is
// true; the CLI itself also stays inert without ~/.config/trakt-cli/sync.yaml.
const SYNC_TOOL: ToolDef = {
	name: 'trakt_sync',
	command: 'sync',
	description:
		'Import watched items from streaming services (Apple TV Up Next, Netflix, HBO Max, Paramount+) into Trakt history, using the sync config at ~/.config/trakt-cli/sync.yaml. Skips denied titles, items older than the configured window, and anything already in history. Shows not yet in history and all movies are returned in pending_review and NOT written; ask the user before approving any. Use dry_run first to preview.',
	parameters: Type.Object({
		dry_run: Type.Optional(
			Type.Boolean({ description: 'Preview what would be added without writing (default false)' })
		),
	}),
};

function preferenceTools(): ToolDef[] {
	const result: ToolDef[] = [];
	for (const resource of ['ratings', 'favorites']) {
		const kinds = resource === 'ratings' ? ['movie', 'show', 'season', 'episode'] : ['movie', 'show'];
		result.push({
			name: `trakt_${resource}`, command: resource,
			description: `Read one page of your ${resource}, preserving returned timestamps, IDs and metadata. Read-only; follow pagination for complete coverage. Uses the currently configured single Trakt login; named accounts are not yet supported.`,
			parameters: Type.Object({
				type: Type.Optional(Type.Union([...kinds, 'all'].map(value => Type.Literal(value)), { description: 'Media type; default all' })),
				page: Type.Optional(Type.Integer({ minimum: 1 })),
				limit: Type.Optional(Type.Integer({ minimum: 1, maximum: 1000 })),
			}),
		});
		for (const action of [resource === 'ratings' ? 'set' : 'add', 'remove']) {
			const properties: Record<string, TSchema> = {
				type: Type.Union(kinds.map(value => Type.Literal(value))),
				ids: Type.Array(Type.String({ pattern: '^[1-9][0-9]*$' }), { minItems: 1, maxItems: 10000, uniqueItems: true, description: 'Exact Trakt MEDIA IDs as strings' }),
				apply: Type.Optional(Type.Boolean({ default: false, description: 'Explicitly execute the authorized write; default offline preview' })),
				expect_user: Type.Optional(Type.String({ minLength: 1, description: 'Required with apply=true: account slug must match fresh token identity before any write' })),
			};
			if (resource === 'ratings' && action === 'set') {
				properties.rating = Type.Integer({ minimum: 1, maximum: 10, description: 'New rating; may replace the existing rating' });
				properties.rated_at = Type.Optional(Type.String({ description: 'Optional ISO8601 timestamp; omitted uses server time' }));
			}
			result.push({
				name: `trakt_${resource === 'ratings' ? 'rating' : 'favorite'}_${action}`, command: resource, subcommand: action,
				description: `${action} ${resource} using exact Trakt media IDs. Defaults to offline preview with no login/network. Explicit apply=true plus matching expect_user is required to write. Preview and obtain user authorization before applying. No automatic retries; inspect partial/uncertain outcomes. Favorites support movies/shows only. Does not change history or lists.`,
				parameters: Type.Object(properties),
			});
		}
	}
	return result;
}

// Tool metadata — each maps to a CLI subcommand
const TOOLS: ToolDef[] = [
	...preferenceTools(),
	{
		name: 'trakt_list_create', command: 'lists', subcommand: 'create',
		description: 'Create an owned custom list. Defaults to offline preview; apply=true explicitly writes to the authenticated account. Privacy defaults private. Preview first and obtain user authorization for the write. Do not blindly retry after an uncertain outcome.',
		parameters: Type.Object({
			name: Type.String({ minLength: 1, description: 'New list name' }),
			description: Type.Optional(Type.String({ description: 'List description' })),
			privacy: Type.Optional(Type.Union(['private', 'link', 'friends', 'public'].map(value => Type.Literal(value)), { description: 'Privacy; default private' })),
			apply: Type.Optional(Type.Boolean({ default: false, description: 'Explicitly execute the write; otherwise preview without login or network' })),
		}),
	},
	...(['add', 'remove'] as const).map(action => ({
		name: `trakt_list_${action}`, command: 'lists', subcommand: action,
		description: `${action === 'add' ? 'Add' : 'Remove'} media on an owned custom list by Trakt MEDIA IDs. Defaults to offline preview; apply=true explicitly writes. No title guessing, whole-list deletion or history edits. Inspect partial/not_found results; do not automatically retry uncertain writes.`,
		parameters: Type.Object({
			list: Type.String({ minLength: 1, description: 'Owned list ID or slug' }),
			type: Type.Union(['movie', 'show', 'season', 'episode'].map(value => Type.Literal(value))),
			ids: Type.Array(Type.String({ pattern: '^[1-9][0-9]*$' }), { minItems: 1, maxItems: 10000, uniqueItems: true, description: 'Trakt MEDIA IDs as strings, not list-entry IDs' }),
			apply: Type.Optional(Type.Boolean({ default: false, description: 'Execute the explicitly authorized write; default preview' })),
		}),
	})),
	{
		name: 'trakt_list_notes', command: 'lists', subcommand: 'notes',
		description: 'Edit notes for one owned custom-list ENTRY ID, not a media ID. VIP only; empty notes clears them. Defaults offline preview; apply=true explicitly writes after VIP verification. Preview and obtain user authorization before applying.',
		parameters: Type.Object({
			list: Type.String({ minLength: 1 }),
			item: Type.String({ pattern: '^[1-9][0-9]*$', description: 'List ENTRY ID from trakt_list_items' }),
			notes: Type.String({ maxLength: 500, description: 'New notes; explicitly empty clears them (VIP)' }),
			apply: Type.Optional(Type.Boolean({ default: false })),
		}),
	},
	{
		name: 'trakt_list_reorder', command: 'lists', subcommand: 'reorder',
		description: 'Reorder an owned custom list using ALL current list ENTRY IDs, not media IDs. Defaults offline preview. apply=true verifies complete membership and stable metadata before writing; no transactional lock is available. The list display sort may need rank to show manual order; this command does not change that setting. Obtain user authorization before applying.',
		parameters: Type.Object({
			list: Type.String({ minLength: 1 }),
			rank: Type.Array(Type.String({ pattern: '^[1-9][0-9]*$' }), { minItems: 1, maxItems: 10000, uniqueItems: true, description: 'Complete list ENTRY IDs in desired order' }),
			apply: Type.Optional(Type.Boolean({ default: false })),
		}),
	},
	{
		name: 'trakt_search',
		command: 'search',
		description:
			'Search Trakt.tv for movies and TV shows. Returns title, year, trakt_id, and imdb.',
		parameters: Type.Object({
			query: Type.String({ description: 'Search query (movie or show title)' }),
			type: Type.Optional(
				Type.Union(
					[Type.Literal('movie'), Type.Literal('show'), Type.Literal('movie,show')],
					{ description: 'Filter by type (default: movie,show)' }
				)
			),
		}),
	},
	{
		name: 'trakt_history',
		command: 'history',
		description:
			'View Trakt.tv watch history. Returns recently watched movies and episodes with timestamps.',
		parameters: Type.Object({
			type: Type.Optional(
				Type.Union([Type.Literal('movies'), Type.Literal('shows')], {
					description: 'Filter by type',
				})
			),
			limit: Type.Optional(Type.Number({ description: 'Items per page (default 10)' })),
			page: Type.Optional(Type.Number({ description: 'Page number' })),
		}),
	},
	{
		name: 'trakt_history_add',
		command: 'history',
		subcommand: 'add',
		description:
			'Mark movies or shows as watched on Trakt.tv. Searches by title and adds to history. Accepts multiple titles.',
		parameters: Type.Object({
			titles: Type.Array(Type.String(), { description: 'Title(s) to mark as watched' }),
			type: Type.Optional(
				Type.Union([Type.Literal('movie'), Type.Literal('show')], {
					description: 'Content type (default: show)',
				})
			),
			watched_at: Type.Optional(
				Type.String({
					description: 'When watched (YYYY-MM-DD or RFC3339, defaults to now)',
				})
			),
		}),
	},
	{
		name: 'trakt_history_remove',
		command: 'history',
		subcommand: 'remove',
		description:
			'Remove movies or shows from Trakt.tv watch history (undo a mark-as-watched). Searches by title; removing a show wipes ALL its episode plays, removing a movie wipes all its watches. There is no per-watch granular removal.',
		parameters: Type.Object({
			titles: Type.Array(Type.String(), {
				description: 'Title(s) to remove from history',
				minItems: 1,
			}),
			type: Type.Optional(
				Type.Union([Type.Literal('movie'), Type.Literal('show')], {
					description: 'Content type (default: show)',
				})
			),
		}),
	},
	{
		name: 'trakt_backup',
		command: 'backup',
		description:
			'Create a new local ZIP covering raw history, lists, ratings, collections, favorites, playback, watched summaries, hidden items, comments/replies, notes, social/like data, saved filters and account snapshots. Includes checksums, counts and explicit API-scope completeness, not guaranteed website-field parity. Collaborated lists include metadata only. Never overwrites files or changes Trakt. Contains private data; use on user request. Failures retain a partial ZIP marked incomplete; do not retry after rate limiting until the stated interval has passed.',
		parameters: Type.Object({
			output: Type.String({ minLength: 1, description: 'New local ZIP path; must not already exist' }),
			limit: Type.Optional(Type.Integer({ minimum: 1, maximum: 1000, description: 'Records per API page (default 1000)' })),
			delay_ms: Type.Optional(Type.Integer({ minimum: 0, description: 'Minimum milliseconds between requests (default 1000)' })),
			max_pages: Type.Optional(Type.Integer({ minimum: 1, description: 'Maximum pages per resource (default 10000); hitting it makes export incomplete' })),
			website_supplement: Type.Optional(Type.String({ minLength: 1, description: 'Local JSON from backup import-supplement; fills only missing website-only account fields, preserves raw API snapshots and records historical provenance' })),
		}),
	},
	{
		name: 'trakt_backup_compare',
		command: 'backup',
		subcommand: 'compare',
		description: 'Read-only local comparison of two backup ZIPs. Validates native manifest checksums/counts/IDs and refuses partial backups; website-layout ZIPs get CRC/JSON/ID checks but unknown completeness. Reports coverage-aware additions, removals and changed field paths, never before/after values. Historical supplements require freshness review. No Trakt login, network calls, restore or file modifications.',
		parameters: Type.Object({
			before: Type.String({ minLength: 1, description: 'Earlier local ZIP path' }),
			after: Type.String({ minLength: 1, description: 'Later local ZIP path' }),
			sample_limit: Type.Optional(Type.Integer({ minimum: 0, maximum: 100, description: 'Maximum ID/field-path samples per change type and resource (default 10)' })),
		}),
	},
	{
		name: 'trakt_lists',
		command: 'lists',
		description:
			'Read your custom lists, including private lists accessible to your authenticated account. Returns original API metadata and pagination. Read-only; not a full account backup.',
		parameters: Type.Object({
			page: Type.Optional(Type.Integer({ minimum: 1, description: 'Page number (default 1)' })),
			limit: Type.Optional(Type.Integer({ minimum: 1, maximum: 1000, description: 'Items per page (default 10)' })),
		}),
	},
	{
		name: 'trakt_list_items',
		command: 'lists',
		subcommand: 'items',
		description:
			'Read one custom list by Trakt ID or slug from trakt_lists. Preserves returned rank, IDs, listed_at, notes and mixed item types. Follow pagination for complete contents. Read-only; only fields returned by the API are available.',
		parameters: Type.Object({
			list: Type.String({ minLength: 1, description: 'Custom list Trakt ID or slug (not its display name)' }),
			page: Type.Optional(Type.Integer({ minimum: 1, description: 'Page number (default 1)' })),
			limit: Type.Optional(Type.Integer({ minimum: 1, maximum: 1000, description: 'Items per page (default 10)' })),
		}),
	},
	{
		name: 'trakt_watchlist',
		command: 'watchlist',
		description:
			'View Trakt.tv watchlist. Returns items the user wants to watch, with type, title, year, and added date.',
		parameters: Type.Object({
			type: Type.Optional(
				Type.Union([Type.Literal('movies'), Type.Literal('shows')], {
					description: 'Filter by type',
				})
			),
			limit: Type.Optional(Type.Number({ description: 'Items per page (default 10)' })),
			page: Type.Optional(Type.Number({ description: 'Page number' })),
		}),
	},
	{
		name: 'trakt_watchlist_add',
		command: 'watchlist',
		subcommand: 'add',
		description:
			'Add movies or shows to the Trakt.tv watchlist. Searches by title and queues matches in one sync call. Accepts multiple titles. Items already on the list are reported under `existing_*` counts.',
		parameters: Type.Object({
			titles: Type.Array(Type.String(), {
				description: 'Title(s) to add to the watchlist',
				minItems: 1,
			}),
			type: Type.Optional(
				Type.Union([Type.Literal('movie'), Type.Literal('show')], {
					description: 'Content type (default: show)',
				})
			),
		}),
	},
	{
		name: 'trakt_watchlist_remove',
		command: 'watchlist',
		subcommand: 'remove',
		description:
			'Remove movies or shows from the Trakt.tv watchlist. Searches by title and removes matches in one sync call. Accepts multiple titles. Items that were not on the list are silent no-ops (deleted_*=0).',
		parameters: Type.Object({
			titles: Type.Array(Type.String(), {
				description: 'Title(s) to remove from the watchlist',
				minItems: 1,
			}),
			type: Type.Optional(
				Type.Union([Type.Literal('movie'), Type.Literal('show')], {
					description: 'Content type (default: show)',
				})
			),
		}),
	},
	{
		name: 'trakt_calendar',
		command: 'calendar',
		description:
			'Show upcoming episodes of shows you watch. Returns forward-looking airings within a date window. Use for proactive "what airs next?" workflows — this is the only trakt tool that returns future data.',
		parameters: Type.Object({
			days: Type.Optional(
				Type.Number({ description: 'Number of days to look ahead (default 7)' })
			),
			start: Type.Optional(
				Type.String({ description: 'Start date YYYY-MM-DD (defaults to today)' })
			),
			new: Type.Optional(
				Type.Boolean({
					description: 'Only return series premieres (S01E01 airings)',
				})
			),
		}),
	},
	{
		name: 'trakt_progress',
		command: 'progress',
		description:
			'Show progress of watchlist TV shows. Returns in-progress shows with episode counts, percentage, and next episode to watch.',
		parameters: Type.Object({
			all: Type.Optional(
				Type.Boolean({
					description: 'Include not_started and completed shows (default: in-progress only)',
				})
			),
		}),
	},
	{
		name: 'trakt_auth',
		command: 'auth',
		description:
			'Set up Trakt.tv authentication. Initiates OAuth device flow using configured client credentials. Only needed for initial setup.',
		parameters: Type.Object({}),
	},
];

/** Helper to build a tool result with the required content + details shape. */
function toolResult(text: string) {
	return {
		content: [{ type: 'text' as const, text }],
		details: undefined,
	};
}

/**
 * Resolve the CLI binary path:
 * 1. Plugin config cliPath
 * 2. Env var TRAKT_CLI_PATH
 * 3. PATH lookup (try both `trakt-cli` and `trakt-plugin`)
 */
function resolveCliPath(config?: PluginConfig): string {
	if (config?.cliPath && existsSync(config.cliPath)) {
		return config.cliPath;
	}

	const envPath = process.env.TRAKT_CLI_PATH;
	if (envPath && existsSync(envPath)) {
		return envPath;
	}

	for (const name of ['trakt-cli', 'trakt-plugin']) {
		const found = whichBinary(name);
		if (found) return found;
	}

	throw new Error(
		'trakt-cli not found. Install with: go install github.com/omarshahine/trakt-plugin@latest\n' +
			'Or set TRAKT_CLI_PATH or configure cliPath in plugin settings.'
	);
}

function isAuthConfigured(): boolean {
	return existsSync(join(homedir(), '.trakt.yaml'));
}

/**
 * Build CLI arguments from tool parameters.
 * Always appends --json for machine-readable output.
 */
function buildCliArgs(
	command: string,
	subcommand: string | undefined,
	params: Record<string, unknown>,
	config?: PluginConfig
): string[] {
	const args: string[] = [command];

	if (command === 'auth') {
		if (config?.clientId) args.push('--client-id', config.clientId);
		if (config?.clientSecret) args.push('--client-secret', config.clientSecret);
		return args;
	}

	if (subcommand) args.push(subcommand);

	if (command === 'search') {
		if (params.query) args.push(String(params.query));
	}

	if (command === 'history' && subcommand === 'add') {
		const titles = params.titles as string[] | undefined;
		if (titles) {
			if (params.type) args.push('--type', String(params.type));
			if (params.watched_at) args.push('--watched-at', String(params.watched_at));
			for (const title of titles) {
				args.push(title);
			}
			args.push('--json');
			return args;
		}
	}

	// Title-array commands (watchlist add/remove, history remove) all share
	// the same shape: search by title, POST the matches. Only `history add`
	// needs special handling for --watched-at.
	const titleArrayCommands: Array<[string, string]> = [
		['watchlist', 'add'],
		['watchlist', 'remove'],
		['history', 'remove'],
	];
	if (titleArrayCommands.some(([c, s]) => c === command && s === subcommand)) {
		const titles = params.titles as string[] | undefined;
		if (titles) {
			if (params.type) args.push('--type', String(params.type));
			for (const title of titles) {
				args.push(title);
			}
			args.push('--json');
			return args;
		}
	}

	const skipKeys = new Set(['query', 'titles']);
	for (const [key, value] of Object.entries(params)) {
		if (skipKeys.has(key) || value === undefined || value === null || value === false) continue;
		const flag = `--${key.replace(/_/g, '-')}`;
		if (typeof value === 'boolean') {
			args.push(flag);
		} else {
			args.push(flag, String(value));
		}
	}

	args.push('--json');
	return args;
}

export default definePluginEntry({
	id: 'trakt-tools',
	name: 'Trakt',
	description: 'Track movies and TV shows using trakt.tv',

	register(api) {
		const config = api.pluginConfig as PluginConfig | undefined;
		const tools = config?.syncEnabled ? [...TOOLS, SYNC_TOOL] : TOOLS;

		let cliPath: string;
		try {
			cliPath = resolveCliPath(config);
		} catch (error) {
			// Defer error to tool execution time — plugin still loads
			const errorMessage = error instanceof Error ? error.message : String(error);

			for (const tool of tools) {
				api.registerTool({
					name: tool.name,
					label: tool.name,
					description: tool.description,
					parameters: tool.parameters,
					async execute() {
						return toolResult(
							JSON.stringify({ success: false, error: errorMessage }, null, 2)
						);
					},
				});
			}
			return;
		}

		for (const tool of tools) {
			api.registerTool({
				name: tool.name,
				label: tool.name,
				description: tool.description,
				parameters: tool.parameters,

				async execute(_id: string, params: Record<string, unknown>) {
					const localComparison = tool.command === 'backup' && tool.subcommand === 'compare';
					const listPreview = tool.command === 'lists' && ['create', 'add', 'remove', 'notes', 'reorder'].includes(tool.subcommand ?? '') && params.apply !== true;
					const preferencePreview = ['ratings', 'favorites'].includes(tool.command) && ['set', 'add', 'remove'].includes(tool.subcommand ?? '') && params.apply !== true;
					if (tool.command !== 'auth' && !localComparison && !listPreview && !preferencePreview && !isAuthConfigured()) {
						return toolResult(
							JSON.stringify(
								{
									success: false,
									error:
										'Trakt auth not configured. Run trakt_auth first, or manually: trakt-cli auth --client-id X --client-secret Y',
								},
								null,
								2
							)
						);
					}

					try {
						const args = buildCliArgs(tool.command, tool.subcommand, params, config);
						// 30s was too tight for `progress`, which iterates ~50 API calls
						// (watchlist + watched + N× GetShowProgress) and empirically runs
						// 35-45s for users with substantial watch history. 120s gives
						// generous headroom. maxBuffer bumped to 4MB to fit the full
						// progress --all response (completed + in_progress + not_started).
						// Follow-up: parallelize GetShowProgress loop or add a --filter
						// flag so per-show progress checks don't hammer 50 API calls.
						const { stdout } = await runCli(cliPath, args, {
							// sync walks several services plus per-show history lookups.
							timeout: ['sync', 'backup'].includes(tool.command) ? 600_000 : 120_000,
							maxBuffer: 4 * 1024 * 1024,
						});

						let result: unknown;
						try {
							result = JSON.parse(stdout);
						} catch {
							result = { output: stdout.trim() };
						}

						return toolResult(JSON.stringify(result, null, 2));
					} catch (error: unknown) {
						const message = error instanceof Error ? error.message : String(error);
						const stderr =
							error && typeof error === 'object' && 'stderr' in error
								? String((error as { stderr: unknown }).stderr).trim()
								: '';
						const errorOutput = stderr ? `${message}\n\nstderr: ${stderr}` : message;

						// Detect rate-limit markers emitted by the Go CLI's RateLimitError
						// (see api/api.go). The CLI formats the error as:
						//   rate limited by trakt api: <status> (path=<path> retry_after=<N>)
						// Either stdout or stderr may carry it depending on how the CLI
						// exited. Surface it as a structured response so the agent can
						// back off instead of retrying.
						const rateLimitMatch = errorOutput.match(
							/rate limited by trakt api[^\n]*?retry_after=(\d+)/i
						) || errorOutput.match(/rate limited by trakt api/i);
						if (rateLimitMatch) {
							const retryAfter = rateLimitMatch[1]
								? parseInt(rateLimitMatch[1], 10)
								: null;
							return toolResult(
								JSON.stringify(
									{
										success: false,
										rate_limited: true,
										retry_after_seconds: retryAfter,
										error:
											'Trakt API rate-limited this request' +
											(retryAfter != null ? ` (retry after ~${retryAfter}s)` : '') +
											'. Do NOT retry immediately — wait out the window before calling any trakt_* tool again, and avoid falling back to other trakt tools (they share the same rate limit bucket).',
										raw: errorOutput,
									},
									null,
									2
								)
							);
						}

						// The CLI exits nonzero for partial writes, but stdout retains
						// structured outcome/counts so callers can inspect before retrying.
						const listWrite = tool.command === 'lists' && ['create', 'add', 'remove', 'notes', 'reorder'].includes(tool.subcommand ?? '');
						const preferenceWrite = ['ratings', 'favorites'].includes(tool.command) && ['set', 'add', 'remove'].includes(tool.subcommand ?? '');
						if ((listWrite || preferenceWrite) && params.apply === true && error && typeof error === 'object' && 'stdout' in error) {
							try {
								const operation = JSON.parse(String((error as { stdout: unknown }).stdout));
								if (operation?.action === tool.subcommand && (!preferenceWrite || operation.resource === tool.command) && ['not_attempted', 'unknown', 'partial'].includes(operation.outcome)) {
									return toolResult(JSON.stringify({ success: false, error: errorOutput, operation }, null, 2));
								}
							} catch { /* Fall through to the ordinary error without replay. */ }
						}
						return toolResult(
							JSON.stringify({ success: false, error: errorOutput }, null, 2)
						);
					}
				},
			});
		}
	},
});
