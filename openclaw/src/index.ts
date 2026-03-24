/**
 * OpenClaw plugin entry for trakt-cli.
 *
 * Registers tools that shell out to the `trakt-cli` binary.
 * Each tool maps to a CLI subcommand (search, history, watchlist, progress).
 */

import { definePluginEntry } from 'openclaw/plugin-sdk/plugin-entry';
import { Type } from '@sinclair/typebox';
import { execFileSync, execFile } from 'child_process';
import { promisify } from 'util';
import { existsSync } from 'fs';
import { homedir } from 'os';
import { join } from 'path';

const execFileAsync = promisify(execFile);

interface PluginConfig {
	cliPath?: string;
	clientId?: string;
	clientSecret?: string;
}

interface ToolDef {
	name: string;
	command: string;
	subcommand?: string;
	description: string;
	parameters: ReturnType<typeof Type.Object>;
}

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
 * Look up a binary on PATH, cross-platform.
 */
function whichBinary(name: string): string | null {
	const cmd = process.platform === 'win32' ? 'where.exe' : 'which';
	try {
		const result = execFileSync(cmd, [name], { encoding: 'utf8' }).trim();
		const first = result.split('\n')[0]?.trim();
		return first || null;
	} catch {
		return null;
	}
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
	id: 'trakt',
	name: 'Trakt',
	description: 'Track movies and TV shows using trakt.tv',

	register(api) {
		const config = api.pluginConfig as PluginConfig | undefined;

		let cliPath: string;
		try {
			cliPath = resolveCliPath(config);
		} catch (error) {
			// Defer error to tool execution time — plugin still loads
			const errorMessage = error instanceof Error ? error.message : String(error);

			for (const tool of TOOLS) {
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

		for (const tool of TOOLS) {
			api.registerTool({
				name: tool.name,
				label: tool.name,
				description: tool.description,
				parameters: tool.parameters,

				async execute(_id: string, params: Record<string, unknown>) {
					if (tool.command !== 'auth' && !isAuthConfigured()) {
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
						const { stdout } = await execFileAsync(cliPath, args, {
							encoding: 'utf8',
							timeout: 30_000,
							maxBuffer: 1024 * 1024,
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

						return toolResult(
							JSON.stringify({ success: false, error: errorOutput }, null, 2)
						);
					}
				},
			});
		}
	},
});
