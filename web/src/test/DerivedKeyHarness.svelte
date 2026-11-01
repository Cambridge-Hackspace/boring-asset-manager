<script lang="ts">
  // DerivedKey under a label, as a form has it, for its tests.
  import { untrack } from 'svelte';
  import DerivedKey from '../components/DerivedKey.svelte';

  let { follows, start = '', warning = '' }: { follows: boolean; start?: string; warning?: string } = $props();
  let label = $state('');
  let key = $state(untrack(() => start));
</script>

<label for="q-label">Label</label>
<input id="q-label" bind:value={label} />
<DerivedKey id="q-key" bind:value={key} from={label} {follows} what="key of question 1" lead="Used as" format={(k) => `{${k}}`} {warning} />
<output data-testid="key">{key}</output>
