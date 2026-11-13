import { chmod, mkdir, readFile, writeFile } from 'node:fs/promises'
import { join } from 'node:path'

import { voxError } from './cli-error.ts'
import { voxHome } from './paths.ts'

export type StateFile = {
	last_voice?: string
	last_lang?: string
}

export type AppConfig = {
	dir: string
	state: StateFile
}

export async function loadConfig(dir = voxHome()): Promise<AppConfig> {
	await mkdir(dir, { recursive: true })
	await mkdir(join(dir, 'voices'), { recursive: true })
	await mkdir(join(dir, 'cache'), { recursive: true })

	const state = await readJson<StateFile>(join(dir, 'state.json'), {})
	return { dir, state }
}

export async function saveState(app: AppConfig): Promise<void> {
	await writePrivateJson(join(app.dir, 'state.json'), app.state)
}

async function readJson<T>(path: string, fallback: T): Promise<T> {
	let text: string
	try {
		text = await readFile(path, 'utf8')
	} catch (error) {
		if (isNotFound(error)) return fallback
		throw voxError(
			'io_error',
			`cannot read ${path}: ${error instanceof Error ? error.message : String(error)}`,
		)
	}
	try {
		return JSON.parse(text) as T
	} catch (error) {
		throw voxError(
			'io_error',
			`${path} is not readable: ${error instanceof Error ? error.message : String(error)}`,
		)
	}
}

async function writePrivateJson(path: string, value: unknown): Promise<void> {
	await writeFile(path, JSON.stringify(value, null, 2) + '\n', { mode: 0o600 })
	await chmod(path, 0o600)
}

function isNotFound(error: unknown): boolean {
	return (
		typeof error === 'object' &&
		error !== null &&
		'code' in error &&
		error.code === 'ENOENT'
	)
}
