/**
 * OpenClaw plugin entry for trakt-cli.
 *
 * Registers tool factories that shell out to the `trakt-cli` binary.
 * Each tool maps to a CLI subcommand (search, history, watchlist, progress).
 * Uses the factory pattern so each agent gets per-workspace config resolution.
 */

import { execFileSync, execFile } from 'child_process';
import { promisify } from 'util';
import { existsSync } from 'fs';
import { homedir } from 'os';
import { join } from 'path';

const execFileAsync = promisify(execFile);

// OpenClaw plugin config (from openclaw.plugin.json configSchema)
interface PluginConfig {
	cliPath?: string;
	clientId?: string;
	clientSecret?: string;
}

// OpenClaw tool result content block
interface TextContent {
	type: 'text';
	text: string;
}

// OpenClaw tool definition
interface OpenClawToolDefinition {
	name: string;
	label: string;
	description: string;
	parameters: Record<string, unknown>;
	execute: (
		toolCallId: string,
		params: Record<string, unknown>,
		signal?: AbortSignal,
		onUpdate?: (partialResult: unknown) => void
	) => Promise<{ content: TextContent[] }>;
}

// Context provided to factory functions
interface OpenClawPluginToolContext {
	config?: Record<string, unknown>;
	workspaceDir?: string;
	agentDir?: string;
}

// Factory function type
type OpenClawPluginToolFactory = (
	ctx: OpenClawPluginToolContext
) => OpenClawToolDefinition | OpenClawToolDefinition[] | null | undefined;

// OpenClaw plugin registration interface
interface OpenClawContext {
	config?: PluginConfig;
	registerTool(toolOrFactory: OpenClawToolDefinition | OpenClawPluginToolFactory): void;
}

// Tool definitions — each maps to a CLI subcommand
const TOOLS: Array<{
	name: string;
	command: string;
	subcommand?: string;
	description: string;
	parameters: Record<string, unknown>;
}> = [
	{
		name: 'trakt_search',
		command: 'search',
		description:
			'Search Trakt.tv for movies and TV shows. Returns title, year, trakt_id, imdb, and relevance score.',
		parameters: {
			type: 'object',
			properties: {
				query: {
					type: 'string',
					description: 'Search query (movie or show title)'
				},
				type: {
					type: 'string',
					enum: ['movie', 'show', 'movie,show'],
					description: 'Filter by type (default: movie,show)'
				}
			},
			required: ['query']
		}
	},
	{
		name: 'trakt_history',
		command: 'history',
		description:
			'View Trakt.tv watch history. Returns recently watched movies and episodes with timestamps.',
		parameters: {
			type: 'object',
			properties: {
				type: {
					type: 'string',
					enum: ['movies', 'shows'],
					description: 'Filter by type'
				},
				limit: {
					type: 'number',
					description: 'Items per page (default 10)'
				},
				page: {
					type: 'number',
					description: 'Page number'
				}
			}
		}
	},
	{
		name: 'trakt_history_add',
		command: 'history',
		subcommand: 'add',
		description:
			'Mark movies or shows as watched on Trakt.tv. Searches by title and adds to history. Accepts multiple titles.',
		parameters: {
			type: 'object',
			properties: {
				titles: {
					type: 'array',
					items: { type: 'string' },
					description: 'Title(s) to mark as watched'
				},
				type: {
					type: 'string',
					enum: ['movie', 'show'],
					description: 'Content type (default: show)'
				},
				watched_at: {
					type: 'string',
					description: 'When watched (YYYY-MM-DD or RFC3339, defaults to now)'
				}
			},
			required: ['titles']
		}
	},
	{
		name: 'trakt_watchlist',
		command: 'watchlist',
		description:
			'View Trakt.tv watchlist. Returns items the user wants to watch, with type, title, year, and added date.',
		parameters: {
			type: 'object',
			properties: {
				type: {
					type: 'string',
					enum: ['movies', 'shows'],
					description: 'Filter by type'
				},
				limit: {
					type: 'number',
					description: 'Items per page (default 10)'
				},
				page: {
					type: 'number',
					description: 'Page number'
				}
			}
		}
	},
	{
		name: 'trakt_progress',
		command: 'progress',
		description:
			'Show progress of watchlist TV shows. Returns in-progress shows with episode counts, percentage, and next episode to watch.',
		parameters: {
			type: 'object',
			properties: {
				all: {
					type: 'boolean',
					description: 'Include not_started and completed shows (default: in-progress only)'
				}
			}
		}
	},
	{
		name: 'trakt_auth',
		command: 'auth',
		description:
			'Set up Trakt.tv authentication. Initiates OAuth device flow using configured client credentials. Only needed for initial setup.',
		parameters: {
			type: 'object',
			properties: {}
		}
	}
];

/**
 * Resolve the CLI binary path using a discovery chain:
 * 1. Plugin config cliPath
 * 2. Env var TRAKT_CLI_PATH
 * 3. PATH lookup (which trakt-cli)
 * 4. Error with helpful message
 */
function resolveCliPath(config?: PluginConfig): string {
	// 1. Plugin config
	if (config?.cliPath && existsSync(config.cliPath)) {
		return config.cliPath;
	}

	// 2. Env var
	const envPath = process.env.TRAKT_CLI_PATH;
	if (envPath && existsSync(envPath)) {
		return envPath;
	}

	// 3. PATH lookup
	try {
		const result = execFileSync('which', ['trakt-cli'], { encoding: 'utf8' }).trim();
		if (result) return result;
	} catch {
		// Not on PATH
	}

	throw new Error(
		'trakt-cli not found. Install with: go install github.com/omarshahine/trakt-plugin@latest\n' +
			'Or set TRAKT_CLI_PATH or configure cliPath in plugin settings.'
	);
}

/**
 * Check if Trakt auth is configured (~/.trakt.yaml exists).
 */
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

	// Handle auth command specially — uses config credentials
	if (command === 'auth') {
		if (config?.clientId) args.push('--client-id', config.clientId);
		if (config?.clientSecret) args.push('--client-secret', config.clientSecret);
		return args;
	}

	// Add subcommand if present (e.g., history add)
	if (subcommand) args.push(subcommand);

	// Handle search: positional query arg
	if (command === 'search') {
		if (params.query) args.push(String(params.query));
	}

	// Handle history add: positional titles
	if (command === 'history' && subcommand === 'add') {
		const titles = params.titles as string[] | undefined;
		if (titles) {
			// --type and --watched-at go before titles
			if (params.type) args.push('--type', String(params.type));
			if (params.watched_at) args.push('--watched-at', String(params.watched_at));
			for (const title of titles) {
				args.push(title);
			}
			args.push('--json');
			return args;
		}
	}

	// Map remaining params to CLI flags
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

	// Always append --json
	args.push('--json');
	return args;
}

/**
 * OpenClaw plugin activation function.
 * Called by the OpenClaw gateway when the plugin is loaded.
 */
export default function activate(context: OpenClawContext): void {
	const config = context.config;

	let cliPath: string;

	try {
		cliPath = resolveCliPath(config);
	} catch (error) {
		// Defer error to tool execution time — plugin still loads
		const errorMessage = error instanceof Error ? error.message : String(error);

		for (const tool of TOOLS) {
			context.registerTool(() => ({
				name: tool.name,
				label: tool.name,
				description: tool.description,
				parameters: tool.parameters,
				async execute() {
					return {
						content: [
							{
								type: 'text' as const,
								text: JSON.stringify({ success: false, error: errorMessage }, null, 2)
							}
						]
					};
				}
			}));
		}
		return;
	}

	for (const tool of TOOLS) {
		context.registerTool((_ctx: OpenClawPluginToolContext) => ({
			name: tool.name,
			label: tool.name,
			description: tool.description,
			parameters: tool.parameters,

			async execute(
				_toolCallId: string,
				params: Record<string, unknown>
			) {
				// Check auth for non-auth tools
				if (tool.command !== 'auth' && !isAuthConfigured()) {
					return {
						content: [
							{
								type: 'text' as const,
								text: JSON.stringify({
									success: false,
									error: 'Trakt auth not configured. Run trakt_auth first, or manually: trakt-cli auth --client-id X --client-secret Y'
								}, null, 2)
							}
						]
					};
				}

				try {
					const args = buildCliArgs(tool.command, tool.subcommand, params, config);
					const { stdout } = await execFileAsync(cliPath, args, {
						encoding: 'utf8',
						timeout: 30_000,
						maxBuffer: 1024 * 1024 // 1MB
					});

					// Try to parse as JSON for structured output
					let result: unknown;
					try {
						result = JSON.parse(stdout);
					} catch {
						result = { output: stdout.trim() };
					}

					return {
						content: [
							{
								type: 'text' as const,
								text: JSON.stringify(result, null, 2)
							}
						]
					};
				} catch (error: unknown) {
					const message = error instanceof Error ? error.message : String(error);
					const stderr =
						error && typeof error === 'object' && 'stderr' in error
							? String((error as { stderr: unknown }).stderr).trim()
							: '';
					const errorOutput = stderr ? `${message}\n\nstderr: ${stderr}` : message;

					return {
						content: [
							{
								type: 'text' as const,
								text: JSON.stringify({ success: false, error: errorOutput }, null, 2)
							}
						]
					};
				}
			}
		}));
	}
}
