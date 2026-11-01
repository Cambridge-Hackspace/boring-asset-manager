<script lang="ts">
  // /flows/:slug/poster: a page to print and put up where a flow is used,
  // with its name, its QR code and one line of instructions (docs/flows.md);
  // or the same as a label template, for a label printer.
  import { onMount } from 'svelte';
  import { request } from '../../lib/api';
  import { messageOf } from '../../lib/errors';
  import { navigate } from '../../lib/router.svelte';
  import { session } from '../../lib/session.svelte';
  import type { Flow } from '../../lib/flows';
  import ErrorMessage from '../../components/ErrorMessage.svelte';

  let { params }: { params: Record<string, string> } = $props();
  const slug = $derived(params.slug ?? '');

  let flow = $state<Flow | null>(null);
  let error = $state('');
  const address = $derived(`${location.origin}/f/${encodeURIComponent(slug)}`);

  onMount(async () => {
    try {
      flow = (await request<{ flow: Flow }>('GET', `/flows/${encodeURIComponent(slug)}`)).flow;
    } catch (err) {
      error = messageOf(err);
    }
  });

  // The poster as a label template: its code beside its name and line, on
  // a 62 × 29 mm label, to adjust in the template editor and print there.
  async function makeTemplate() {
    if (!flow) return;
    error = '';
    try {
      const res = await request<{ template: { id: string } }>('POST', '/templates', {
        name: `Poster: ${flow.name}`,
        units: 'mm',
        width: 62,
        height: 29,
        dpi: 300,
        elements: [
          { type: 'generator', generator_type: 'qr', x: 1, y: 1, width: 27, height: 27, content_binding: address },
          { type: 'text', x: 30, y: 2, width: 31, height: 11, font_size: 11, content_binding: flow.name },
          { type: 'text', x: 30, y: 14, width: 31, height: 13, font_size: 7, content_binding: flow.definition.description || 'Scan to start.' },
        ],
      });
      navigate(`/templates/${encodeURIComponent(res.template.id)}/edit`);
    } catch (err) {
      error = messageOf(err);
    }
  }
</script>

<div class="mb-4 flex flex-wrap items-center justify-between gap-2 print:hidden">
  <a class="inline-flex min-h-11 items-center text-sm underline" href="/flows">Back to flows</a>
  <span class="flex flex-wrap gap-2">
    <button type="button" class="btn-primary" onclick={() => window.print()}>Print</button>
    {#if session.can('templates.manage')}<button type="button" class="btn-secondary" disabled={!flow} onclick={makeTemplate}>Make a label template</button>{/if}
  </span>
</div>
<ErrorMessage {error} />

{#if flow}
  <article class="mx-auto max-w-xl space-y-6 rounded-lg border border-zinc-300 bg-white p-8 text-center text-zinc-900 print:border-0">
    <h1 class="text-4xl font-bold">{flow.name}</h1>
    <img class="mx-auto w-72 max-w-full" src="/api/v1/flows/{encodeURIComponent(flow.slug)}/qr.svg" alt="QR code opening {flow.name}" />
    {#if flow.definition.description}<p class="text-2xl">{flow.definition.description}</p>{/if}
    <p class="font-mono text-sm break-all">{address}</p>
  </article>
{/if}
