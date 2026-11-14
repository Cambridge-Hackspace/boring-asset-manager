<script lang="ts">
  // A query's mistake, shown under the search box: the query with the part
  // at fault underlined, and what is wrong with it.
  import type { QueryProblem } from '../lib/query';

  let { query, problem, id }: { query: string; problem: QueryProblem; id?: string } = $props();

  const before = $derived(query.slice(0, problem.start));
  const at = $derived(query.slice(problem.start, problem.end));
  const after = $derived(query.slice(problem.end));
</script>

<div {id} class="query-problem mt-1 text-sm" role="alert">
  <p class="text-red-700 dark:text-red-400">{problem.message}</p>
  <code class="mt-1 block whitespace-pre-wrap break-all rounded bg-gray-100 px-2 py-1 dark:bg-gray-800"
    >{before}<mark class="query-fault" title={problem.message}>{at || ' '}</mark>{after}</code
  >
</div>

<style>
  .query-fault {
    background: transparent;
    color: inherit;
    text-decoration: underline wavy #dc2626;
    text-decoration-skip-ink: none;
    text-underline-offset: 3px;
  }
</style>
