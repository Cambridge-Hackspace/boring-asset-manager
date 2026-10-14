import { ApiError } from './api';

/** A message for showing a failed request to the user. */
export function messageOf(err: unknown): string {
  if (err instanceof ApiError) return err.message;
  if (err instanceof TypeError) return 'Could not reach BAM!. Check your connection and try again.';
  return err instanceof Error ? err.message : String(err);
}
