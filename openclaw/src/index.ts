/**
 * OpenClaw plugin entry for trakt-cli.
 *
 * Registers tools that shell out to the `trakt-cli` binary.
 * Each tool maps to a CLI subcommand (search, history, watchlist, progress).
 */

import { definePluginEntry } from 'openclaw/plugin-sdk/plugin-entry';
import { Type } from '@sinclair/typebox';
import { Value } from '@sinclair/typebox/value';
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

// Tool metadata — each maps to a CLI subcommand
const TOOLS: ToolDef[] = [
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
			'Set up Trakt.tv authentication without waiting for approval. Start returns verification_url, user_code, session, expiry and polling interval. Show the instructions immediately, then poll that session no faster than the returned interval after the user approves.',
		parameters: Type.Object({
			action: Type.Optional(Type.Union([Type.Literal('start'), Type.Literal('poll')])),
			session: Type.Optional(Type.String({ pattern: '^[0-9a-f]{32}$' })),
		}),
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

/** Keep the tool handler and advertised schema aligned on every invocation path. */
function closedParameters(tool: ToolDef): ToolDef['parameters'] {
	return Type.Object(tool.parameters.properties, { additionalProperties: false });
}

function validParameters(schema: ToolDef['parameters'], params: unknown): params is Record<string, unknown> {
	try {
		return Value.Check(schema, params);
	} catch {
		return false;
	}
}

function validAuthParameters(params: Record<string, unknown>): boolean {
	const action = params.action ?? 'start';
	return action === 'poll' ? typeof params.session === 'string' : params.session === undefined;
}

/** Options precede --, so positional values cannot select CLI flags. */
function buildCliArgs(
	command: string,
	subcommand: string | undefined,
	params: Record<string, unknown>
): string[] {
	const args = [command];
	if (command === 'auth') {
		args.push('--json');
		if (params.action === 'poll') args.push('--session', params.session as string);
		return args;
	}
	if (subcommand) args.push(subcommand);

	const positionals: string[] = [];
	if (command === 'search') positionals.push(params.query as string);
	if (Array.isArray(params.titles)) positionals.push(...(params.titles as string[]));

	for (const [key, value] of Object.entries(params)) {
		if (key === 'query' || key === 'titles' || value === undefined || value === false) continue;
		const flag = `--${key.replace(/_/g, '-')}`;
		if (value === true) args.push(flag);
		else args.push(flag, String(value));
	}
	args.push('--json');
	if (positionals.length) args.push('--', ...positionals);
	return args;
}

/** Create a separate environment per call; never mutate process.env. */
function authEnvironment(config?: PluginConfig): NodeJS.ProcessEnv {
	return {
		...process.env,
		...(config?.clientId ? { TRAKT_CLIENT_ID: config.clientId } : {}),
		...(config?.clientSecret ? { TRAKT_CLIENT_SECRET: config.clientSecret } : {}),
	};
}

/** Old CLIs ignore --json during auth and would wait before returning a code. */
async function supportsAuthSessions(cliPath: string): Promise<boolean> {
	const env: NodeJS.ProcessEnv = {};
	for (const key of ['PATH', 'SystemRoot', 'WINDIR', 'ComSpec']) {
		if (process.env[key] !== undefined) env[key] = process.env[key];
	}
	try {
		const { stdout } = await runCli(cliPath, ['auth', '--help'], {
			timeout: 5_000,
			maxBuffer: 256 * 1024,
			env,
		});
		return /^\s+--session(?:\s|=)/m.test(stdout);
	} catch {
		return false;
	}
}

const AUTH_RESPONSE = Type.Object({
	success: Type.Literal(true),
	status: Type.Union([Type.Literal('pending'), Type.Literal('authenticated')]),
	session: Type.Optional(Type.String({ pattern: '^[0-9a-f]{32}$' })),
	verification_url: Type.Optional(Type.String({ minLength: 1, maxLength: 2048 })),
	user_code: Type.Optional(Type.String({ pattern: '^[A-Za-z0-9-]{1,128}$' })),
	expires_in: Type.Optional(Type.Integer({ minimum: 0 })),
	interval: Type.Optional(Type.Integer({ minimum: 1 })),
}, { additionalProperties: false });

const AUTH_VERIFICATION_ORIGINS = new Set([
	'https://trakt.tv', 'https://www.trakt.tv',
	'https://trakt.tv:443', 'https://www.trakt.tv:443',
]);

/** Accept only Trakt's public verification destination and literal code data. */
function validVerificationURL(value: string, clientSecret?: string): boolean {
	const pathStart = value.indexOf('/', 'https://'.length);
	const origin = pathStart === -1 ? value : value.slice(0, pathStart);
	if (!AUTH_VERIFICATION_ORIGINS.has(origin) || value.includes('?') || value.includes('#') ||
		[...value].some(char => char.charCodeAt(0) < 32 || char.charCodeAt(0) === 127)) return false;
	const url = new URL(value);
	if (url.protocol !== 'https:' || url.username || url.password ||
		(url.hostname !== 'trakt.tv' && url.hostname !== 'www.trakt.tv') ||
		(url.port !== '' && url.port !== '443')) return false;
	// Match the CLI's decoded-path reflection check, not only raw JSON text.
	return !clientSecret || !decodeURIComponent(url.pathname).includes(clientSecret);
}

/** Return only the public device-flow protocol, never arbitrary auth stdout. */
function parseAuthResponse(stdout: string, params: Record<string, unknown>, clientSecret?: string): unknown {
	const result: unknown = JSON.parse(stdout);
	if (!Value.Check(AUTH_RESPONSE, result)) throw new Error('Invalid auth response');
	if (clientSecret && Object.values(result).some(value => typeof value === 'string' && value.includes(clientSecret))) {
		throw new Error('Invalid auth response');
	}
	const action = params.action ?? 'start';
	if (result.status === 'authenticated') {
		if (action !== 'poll' || Object.keys(result).some(key => key !== 'success' && key !== 'status')) {
			throw new Error('Invalid auth response');
		}
		return result;
	}
	if (!result.session || result.expires_in === undefined || result.interval === undefined) {
		throw new Error('Invalid auth response');
	}
	if (action === 'start') {
		if (!result.verification_url || !result.user_code) throw new Error('Invalid auth response');
		if (!validVerificationURL(result.verification_url, clientSecret)) throw new Error('Invalid auth response');
	} else if (result.session !== params.session || result.verification_url !== undefined || result.user_code !== undefined) {
		throw new Error('Invalid auth response');
	}
	return result;
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
					parameters: closedParameters(tool),
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
			const parameters = closedParameters(tool);
			api.registerTool({
				name: tool.name,
				label: tool.name,
				description: tool.description,
				parameters,

				async execute(_id: string, params: Record<string, unknown>) {
					if (!validParameters(parameters, params) || (tool.command === 'auth' && !validAuthParameters(params))) {
						return toolResult(JSON.stringify({ success: false, error: 'Invalid parameters for this Trakt tool.' }));
					}
					if (tool.command === 'auth' && !await supportsAuthSessions(cliPath)) {
						return toolResult(JSON.stringify({
							success: false,
							error: 'This trakt-cli does not support nonblocking authentication. Update trakt-cli, or run trakt-cli auth locally.',
						}));
					}
					if (tool.command !== 'auth' && !isAuthConfigured()) {
						return toolResult(
							JSON.stringify(
								{
									success: false,
									error:
										'Trakt auth not configured. Start trakt_auth, show its verification instructions, then poll its session. Or run trakt-cli auth locally.',
								},
								null,
								2
							)
						);
					}

					try {
						const args = buildCliArgs(tool.command, tool.subcommand, params);
						const env = tool.command === 'auth' ? authEnvironment(config) : undefined;
						// 30s was too tight for `progress`, which iterates ~50 API calls
						// (watchlist + watched + N× GetShowProgress) and empirically runs
						// 35-45s for users with substantial watch history. 120s gives
						// generous headroom. maxBuffer bumped to 4MB to fit the full
						// progress --all response (completed + in_progress + not_started).
						// Follow-up: parallelize GetShowProgress loop or add a --filter
						// flag so per-show progress checks don't hammer 50 API calls.
						const { stdout } = await runCli(cliPath, args, {
							// sync walks several services plus per-show history lookups.
							timeout: tool.command === 'sync' ? 600_000 : 120_000,
							maxBuffer: 4 * 1024 * 1024,
							env,
						});

						if (tool.command === 'auth') {
							return toolResult(JSON.stringify(parseAuthResponse(stdout, params, env?.TRAKT_CLIENT_SECRET), null, 2));
						}

						let result: unknown;
						try {
							result = JSON.parse(stdout);
						} catch {
							result = { output: stdout.trim() };
						}

						return toolResult(JSON.stringify(result, null, 2));
					} catch (error: unknown) {
						// Authentication diagnostics can contain provider credentials.
						// Never inspect or return raw authentication failures to an agent.
						if (tool.command === 'auth') {
							return toolResult(JSON.stringify({
								success: false,
								error: 'Trakt authentication failed. Run trakt-cli auth separately and inspect its output locally.',
							}));
						}
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

						return toolResult(
							JSON.stringify({ success: false, error: errorOutput }, null, 2)
						);
					}
				},
			});
		}
	},
});
