import { createLatch } from '@celados/argc-latch'
import { addGlobalDefs, env, secret } from '@celados/argc/ref'
import * as v from 'valibot'

import { voxError } from './cli-error.ts'

/**
 * Provider keys are `secret()` input fields (argc 10, values by reference).
 * Callers pass a literal or `{ ref: 'env:…' | 'bw://…' }`; omitted, the default
 * chain runs: env var first (CI, one-off overrides), then the celados-agent
 * vault item. Only the command being executed ever reads a source.
 */
const resolvers = [env(), createLatch()]
const key = v.pipe(v.string(), v.trim(), v.minLength(1))

export const providers = {
	dashscope: {
		label: 'DashScope API key',
		description:
			'Alibaba Model Studio (DashScope) API key — ASR fun/qwen, TTS, voices, vocab',
		default: [
			'env:DASHSCOPE_API_KEY',
			'bw://7c38804e-df94-444c-8897-efcde8a77339/凭据',
		],
	},
	mimo: {
		label: 'MiMo API key',
		description: 'Xiaomi MiMo API key — ASR model mimo',
		default: [
			'env:MIMO_API_KEY',
			'bw://52f369e9-e50d-4e49-89e7-a8901c944f41/api key',
		],
	},
} as const
export type Provider = keyof typeof providers

const keyField = (provider: Provider) =>
	secret(key, resolvers, {
		description: providers[provider].description,
		default: providers[provider].default,
	})

const chains = (keys: readonly Provider[]) =>
	keys
		.map((name) => `${name} ${providers[name].default.join(' → ')}`)
		.join('; ')

/**
 * Both key fields are named once (`addGlobalDefs`): `@schema` declares
 * `type DashScopeKey` / `type ProviderKey` with grammar and default chain one
 * time, and each command shows `apiKey?: DashScopeKey | ProviderKey`.
 */
const DashScopeKey = keyField('dashscope')
const ProviderKey = secret(key, resolvers, {
	description: `Key for the provider the command calls (DashScope; MiMo for hear model mimo). Omitted → read only when a provider is actually called — stored runs and cached audio need none: ${chains(['dashscope', 'mimo'])}`,
})
addGlobalDefs({ DashScopeKey, ProviderKey })

/** Commands that always call DashScope: the key resolves during validation. */
export const dashscopeAuth = { apiKey: DashScopeKey }

/**
 * Commands that may not need a key (stored run, cached audio, keyless listing)
 * take an optional override and resolve the default chain only when a provider
 * is actually called — a stored replay stays offline and never waits on the vault.
 */
export const lazyAuth = { apiKey: v.optionalAsync(ProviderKey) }

export type KeyProbe =
	| { available: true; value: string }
	| { available: false; tried: string }

/** Try one provider's default chain. Messages name references, never values. */
export async function probeKey(provider: Provider): Promise<KeyProbe> {
	const result = await v.safeParseAsync(keyField(provider), undefined)
	if (result.success) return { available: true, value: result.output }
	return {
		available: false,
		tried: result.issues.map((issue) => issue.message).join('; '),
	}
}

/** The caller's key, else the provider's default chain; a miss is `key_unavailable`. */
export async function requireKey(
	provider: Provider,
	given: string | undefined,
): Promise<string> {
	if (given !== undefined) return given
	const probe = await probeKey(provider)
	if (probe.available) return probe.value
	throw voxError(
		'key_unavailable',
		`${providers[provider].label}: ${probe.tried}`,
		"vox status shows every provider; or pass apiKey: { ref: 'env:NAME' }",
	)
}
