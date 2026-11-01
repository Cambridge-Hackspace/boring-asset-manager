<script lang="ts">
  // TemplateInput under a label, as the flow editor has it, for its tests.
  import { untrack } from 'svelte';
  import TemplateInput from '../components/TemplateInput.svelte';
  import type { AvailableValue } from '../lib/flowEditor';

  let { start = '', values }: { start?: string; values: AvailableValue[] } = $props();
  let value = $state(untrack(() => start));
</script>

<label class="label" for="t-name">Its name</label>
<TemplateInput id="t-name" name="Its name" labelled {values} bind:value />
<output data-testid="value">{value}</output>
