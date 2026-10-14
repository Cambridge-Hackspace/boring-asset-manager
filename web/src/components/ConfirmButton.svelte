<script lang="ts">
  import { tick } from 'svelte';

  // A destructive button that asks again in place before acting.
  let {
    label,
    confirmLabel = label,
    question,
    onconfirm,
    disabled = false,
  }: {
    label: string;
    confirmLabel?: string;
    question: string;
    onconfirm: () => void | Promise<void>;
    disabled?: boolean;
  } = $props();

  let asking = $state(false);
  let busy = $state(false);
  let confirmButton: HTMLButtonElement | undefined = $state();
  let openButton: HTMLButtonElement | undefined = $state();

  async function ask() {
    asking = true;
    await tick();
    confirmButton?.focus();
  }

  async function cancel() {
    asking = false;
    await tick();
    openButton?.focus();
  }

  async function confirm() {
    busy = true;
    try {
      await onconfirm();
    } finally {
      busy = false;
      asking = false;
    }
  }
</script>

{#if asking}
  <span class="inline-flex flex-wrap items-center gap-2" role="group" aria-label={question}>
    <span class="text-sm">{question}</span>
    <button type="button" class="btn-danger" bind:this={confirmButton} onclick={confirm} disabled={busy}>{confirmLabel}</button>
    <button type="button" class="btn-secondary" onclick={cancel} disabled={busy}>Cancel</button>
  </span>
{:else}
  <button type="button" class="btn-secondary text-red-700 dark:text-red-400" bind:this={openButton} onclick={ask} {disabled}>{label}</button>
{/if}
