import { writeFile } from 'node:fs/promises'

import type { AppHandlers } from './schema.ts'

import { probeKey, providers } from './auth.ts'
import { voxError } from './cli-error.ts'
import { loadConfig } from './config.ts'
import { DashScopeClient, resolveModel, VOCABULARY_QUOTA } from './dashscope.ts'
import { render } from './export.ts'
import { hear } from './hear.ts'
import {
	listRuns,
	loadRun,
	newStore,
	removeAllRuns,
	removeRun,
	resolveSid,
} from './run-store.ts'
import {
	configureSpeakers,
	enrollSpeaker,
	identifyRun,
	listSpeakers,
	loadIdentifiedRun,
	removeSpeaker,
} from './speakers.ts'
import {
	cacheClear,
	cacheStatus,
	listVoices,
	recordVoice,
	removeVoice,
	say,
} from './tts.ts'
import {
	describeIndex,
	loadIndex,
	loadVocabulary,
	listVocabularies,
	pruneVocabularies,
	syncVocabulary,
	vocabNames,
	vocabPath,
	tilde,
} from './vocab.ts'

export const handlers: AppHandlers = {
	status: async () => await providerStatus(),
	hear: async (options) => {
		const app = await loadConfig()
		return await hear(app, options.input)
	},
	speaker: {
		enroll: async (options) =>
			enrollSpeaker((await loadConfig()).dir, options.input),
		list: async () => listSpeakers((await loadConfig()).dir),
		remove: async (options) =>
			removeSpeaker((await loadConfig()).dir, options.input.id),
		config: async (options) =>
			configureSpeakers((await loadConfig()).dir, options.input),
		identify: async (options) =>
			identifyRun((await loadConfig()).dir, options.input),
	},
	session: {
		list: async (options) => {
			const app = await loadConfig()
			return await listRuns(
				newStore(app.dir),
				options.input.file ?? '',
				options.input.limit ?? 20,
			)
		},
		remove: async (options) =>
			await sessionRemove(options.input.sid, options.input.all ?? false),
	},
	export: async (options) => await exportRun(options.input),
	vocab: {
		list: async (options) => await vocabList(options.input.apiKey),
		sync: async (options) => await vocabSync(options.input),
		prune: async (options) =>
			await vocabPrune(options.input.apiKey, options.input.dryRun ?? false),
	},
	say: async (options) => {
		const app = await loadConfig()
		return await say(app, options.input)
	},
	voice: {
		list: async (options) => {
			const app = await loadConfig()
			return await listVoices(app, options.input.apiKey)
		},
		record: async (options) => {
			const app = await loadConfig()
			return await recordVoice(app, options.input)
		},
		remove: async (options) => {
			const app = await loadConfig()
			return await removeVoice(app, options.input.apiKey, options.input.voiceId)
		},
	},
	cache: {
		status: async () => {
			const app = await loadConfig()
			return await cacheStatus(app)
		},
		clear: async () => {
			const app = await loadConfig()
			return await cacheClear(app)
		},
	},
}

async function providerStatus(): Promise<Record<string, unknown>> {
	const entries = await Promise.all(
		(Object.keys(providers) as Array<keyof typeof providers>).map(
			async (provider) => {
				const probe = await probeKey(provider)
				return [
					provider,
					probe.available
						? { available: true }
						: { available: false, tried: probe.tried },
				] as const
			},
		),
	)
	return Object.fromEntries(entries)
}

async function sessionRemove(
	sid: string | undefined,
	all: boolean,
): Promise<Record<string, unknown>> {
	const app = await loadConfig()
	const store = newStore(app.dir)
	if (all && sid) {
		throw voxError(
			'invalid_usage',
			'--all cannot be combined with a run id',
			`vox session.remove --sid ${sid}  ·  vox session.remove --all`,
		)
	}
	if (all) {
		await removeAllRuns(store)
		return { removed: 'all' }
	}
	if (!sid) {
		throw voxError(
			'invalid_usage',
			'no run id given',
			'vox session.remove --sid <sid>  ·  vox session.remove --all',
		)
	}
	const resolved = await resolveSid(store, sid)
	await removeRun(store, resolved)
	return { removed: resolved }
}

async function exportRun(input: {
	sid: string
	format: 'srt' | 'vtt' | 'md' | 'txt' | 'json'
	output?: string
}): Promise<unknown> {
	const app = await loadConfig()
	const store = newStore(app.dir)
	const sid = await resolveSid(store, input.sid)
	let rec
	try {
		rec = await loadRun(store, sid)
	} catch (error) {
		throw voxError(
			'session_not_found',
			`run ${sid} has no stored result: ${error instanceof Error ? error.message : String(error)}`,
		)
	}
	const rendered = render(await loadIdentifiedRun(app.dir, rec), input.format)
	if (input.output) {
		try {
			await writeFile(input.output, rendered)
		} catch (error) {
			throw voxError(
				'io_error',
				`cannot write ${input.output}: ${error instanceof Error ? error.message : String(error)}`,
			)
		}
		return { written: input.output, format: input.format, sid }
	}
	return rendered
}

async function vocabList(apiKey: string | undefined): Promise<unknown> {
	const app = await loadConfig()
	const names = await vocabNames(app.dir)
	const index = await loadIndex(app.dir)
	const vocabularies = []
	for (const name of names) {
		const entry: Record<string, unknown> = {
			name,
			path: tilde(vocabPath(app.dir, name)),
		}
		try {
			const vocab = await loadVocabulary(app.dir, name)
			entry.words = Object.keys(vocab.file.words).length
			if (vocab.file.lang) entry.lang = vocab.file.lang
			const synced = describeIndex(index, name)
			if (Object.keys(synced).length > 0) entry.synced = synced
		} catch (error) {
			entry.error = error instanceof Error ? error.message : String(error)
		}
		vocabularies.push(entry)
	}
	return { quota: await quotaUsage(apiKey), vocabularies }
}

async function quotaUsage(given: string | undefined): Promise<string> {
	let apiKey = given
	if (apiKey === undefined) {
		const probe = await probeKey('dashscope')
		if (!probe.available)
			return 'unknown (DashScope API key unavailable — vox status shows why)'
		apiKey = probe.value
	}
	try {
		const remote = await new DashScopeClient(apiKey).listVocabularies()
		return `${remote.length}/${VOCABULARY_QUOTA}`
	} catch (error) {
		return `unknown (${error instanceof Error ? error.message : String(error)})`
	}
}

async function vocabSync(input: {
	apiKey: string
	name?: string
	all?: boolean
	model?: string
	force?: boolean
}): Promise<unknown> {
	const app = await loadConfig()
	const vendor = input.model ?? 'fun'
	const model = resolveModel(vendor)
	if (!model)
		throw voxError('invalid_usage', `unknown model vendor "${vendor}"`)
	if (input.all && input.name) {
		throw voxError(
			'invalid_usage',
			'--all cannot be combined with a vocabulary name',
		)
	}
	if (!input.all && !input.name) {
		throw voxError(
			'invalid_usage',
			'no vocabulary name given',
			'vox vocab.sync --name meeting  ·  vox vocab.sync --all',
		)
	}

	const client = new DashScopeClient(input.apiKey)
	const targets = input.all
		? await listVocabularies(app.dir)
		: [await loadVocabulary(app.dir, input.name!)]
	const items = []
	for (const vocab of targets) {
		const result = await syncVocabulary(
			client,
			app.dir,
			vocab,
			model,
			input.force ?? false,
		)
		for (const warning of result.warnings)
			console.error(`${vocab.name}: ${warning}`)
		items.push({
			name: vocab.name,
			action: result.action,
			vocabularyId: result.vocabularyId,
			warnings: result.warnings,
		})
	}
	return { items }
}

async function vocabPrune(apiKey: string, dryRun: boolean): Promise<unknown> {
	const app = await loadConfig()
	const orphans = await pruneVocabularies(
		new DashScopeClient(apiKey),
		app.dir,
		dryRun,
	)
	return { orphans, dryRun }
}
