// Helpers for notification channels and their delivery log (docs/notifications.md).

import type { Delivery } from './models';

/** What became of a delivery, in words. */
export function outcome(d: Pick<Delivery, 'status' | 'attempts'>): string {
  const tries = (n: number) => (n === 1 ? '1 attempt' : `${n} attempts`);
  switch (d.status) {
    case 'sent':
      return d.attempts > 1 ? `Sent after ${tries(d.attempts)}` : 'Sent';
    case 'failed':
      return d.attempts > 1 ? `Failed after ${tries(d.attempts)}` : 'Failed';
    default:
      return d.attempts === 0 ? 'Waiting to send' : `Waiting to retry (${tries(d.attempts)} so far)`;
  }
}
