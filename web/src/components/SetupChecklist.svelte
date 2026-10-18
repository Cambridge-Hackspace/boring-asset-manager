<script lang="ts">
  import { onMount } from 'svelte';
  import { request } from '../lib/api';
  import type { Permission } from '../lib/models';
  import { session } from '../lib/session.svelte';

  // Setting BAM! up, for whoever manages it: each step ticked from what is
  // already there, offered to those who may take it. It goes once the steps
  // that are not optional are done, or when hidden.
  interface Step {
    label: string;
    href: string;
    done: boolean;
    optional?: boolean;
  }

  const hiddenKey = 'bam.setup-hidden';
  let steps = $state<Step[] | null>(null);
  let hidden = $state(false);

  const count = <T,>(path: string, key: string, keep: (x: T) => boolean = () => true) =>
    request<Record<string, T[]>>('GET', path).then((r) => (r[key] ?? []).filter(keep).length);

  onMount(async () => {
    try {
      hidden = localStorage.getItem(hiddenKey) === '1';
    } catch {
      // Storage unavailable: shown until done.
    }
    if (hidden) return;
    const can = (p: Permission) => session.can(p);
    try {
      const all = await Promise.all<Step | null>([
        Promise.resolve({ label: 'Give yourself a name, which history shows', href: '/account', done: Boolean(session.user?.name?.trim()) }),
        can('settings.manage')
          ? count('/printers', 'printers').then((n) => ({ label: 'Add a label printer', href: '/printers', done: n > 0 }))
          : null,
        can('categories.manage')
          ? count('/categories', 'categories').then((n) => ({ label: 'Create categories for your assets', href: '/categories', done: n > 0 }))
          : null,
        can('users.manage')
          ? count<{ protected: boolean; public: boolean }>('/roles', 'roles', (r) => !r.protected && !r.public).then((n) => ({
              label: 'Create a role for members, saying what they may do',
              href: '/roles',
              done: n > 0,
            }))
          : null,
        can('users.manage') ? count('/users', 'users').then((n) => ({ label: 'Add the people who use it', href: '/users', done: n > 1 })) : null,
        can('users.manage')
          ? count<{ name: string; category_count: number }>('/scopes', 'scopes', (s) => s.name === 'public' && s.category_count > 0).then((n) => ({
              label: 'Put the public scope on categories anyone may scan without signing in',
              href: '/categories',
              done: n > 0,
              optional: true,
            }))
          : null,
        can('settings.manage')
          ? count('/notifiers', 'channels').then((n) => ({ label: 'Add a notification channel, for running low and overdue loans', href: '/notifications', done: n > 0, optional: true }))
          : null,
      ]);
      steps = all.filter((s): s is Step => s !== null);
    } catch {
      steps = null;
    }
  });

  const finished = $derived(!steps || steps.every((s) => s.done || s.optional));

  function hide() {
    hidden = true;
    try {
      localStorage.setItem(hiddenKey, '1');
    } catch {
      // Storage unavailable: hidden until the page is opened again.
    }
  }
</script>

{#if steps && !hidden && !finished}
  <section class="card mt-6 max-w-2xl space-y-3" aria-labelledby="setup-heading">
    <div class="flex items-start justify-between gap-2">
      <h2 id="setup-heading" class="text-lg font-semibold">Setting up</h2>
      <button type="button" class="btn-secondary text-xs" onclick={hide}>Hide</button>
    </div>
    <ol class="space-y-1 text-sm">
      {#each steps as s (s.href + s.label)}
        <li class="flex items-start gap-2">
          <span aria-hidden="true" class={s.done ? 'text-green-700 dark:text-green-400' : 'text-zinc-500'}>{s.done ? '✓' : '○'}</span>
          <span>
            {#if s.done}<span class="text-zinc-600 line-through dark:text-zinc-400">{s.label}</span><span class="sr-only"> (done)</span>
            {:else}<a class="underline" href={s.href}>{s.label}</a>{/if}{#if s.optional}<span class="text-zinc-600 dark:text-zinc-400"> (optional)</span>{/if}
          </span>
        </li>
      {/each}
    </ol>
  </section>
{/if}
