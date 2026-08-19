import { domainError } from "argc";

export type VoxErrorCode =
  | "not_authenticated"
  | "audio_too_large"
  | "audio_unsupported"
  | "vocab_not_found"
  | "vocab_quota_exceeded"
  | "vocab_model_mismatch"
  | "vocab_index_corrupt"
  | "session_not_found"
  | "session_ambiguous"
  | "api_error"
  | "invalid_usage"
  | "io_error";

export class VoxError extends Error {
  readonly code: VoxErrorCode;
  readonly hint?: string;

  constructor(code: VoxErrorCode, message: string, hint?: string) {
    super(message);
    this.name = code;
    this.code = code;
    this.hint = hint;
  }

  withHint(hint: string): VoxError {
    return new VoxError(this.code, this.message, hint);
  }
}

export function voxError(code: VoxErrorCode, message: string, hint?: string): VoxError {
  return new VoxError(code, message, hint);
}

/**
 * argc renders unrecognized throws as RUNTIME_ERROR and buries the domain code
 * in prose. Convert at the handler boundary so agents can match `code`.
 */
export function toDomainError(error: unknown): unknown {
  if (!(error instanceof VoxError)) return error;
  return domainError(
    error.code,
    error.message,
    error.hint === undefined ? {} : { hint: error.hint },
  );
}

export function withDomainErrors<T>(handlers: T): T {
  if (typeof handlers === "function") {
    return (async (...args: unknown[]) => {
      try {
        return await (handlers as (...a: unknown[]) => unknown)(...args);
      } catch (error) {
        throw toDomainError(error);
      }
    }) as T;
  }
  if (!handlers || typeof handlers !== "object" || Array.isArray(handlers)) return handlers;
  return Object.fromEntries(
    Object.entries(handlers).map(([key, value]) => [key, withDomainErrors(value)]),
  ) as T;
}
