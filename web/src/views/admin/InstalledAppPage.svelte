<!--
  How BAM! looks installed on a phone or computer (docs/installing.md#on-a-phone):
  its name under its icon, the icon, and the colors of the bar above it and
  of its opening screen. Each starts as BAM!'s, or the instance's name.
-->
<script lang="ts">
  import { onMount } from 'svelte';
  import { request } from '../../lib/api';
  import { messageOf } from '../../lib/errors';
  import { installedApp, type AppSettings } from '../../lib/install/app.svelte';
  import type { ImageInfo } from '../../lib/models';
  import { session } from '../../lib/session.svelte';
  import ErrorMessage from '../../components/ErrorMessage.svelte';
  import PageHeader from '../../components/PageHeader.svelte';

  let { params: _params }: { params: Record<string, string> } = $props();

  // BAM!'s colors, as the server defaults them.
  const defaults = { theme_color: '#faf7f0', theme_color_dark: '#0b0b0a', background_color: '#faf7f0' };

  let loaded = $state(false);
  let instanceName = $state('');
  let name = $state('');
  let shortName = $state('');
  let theme = $state(defaults.theme_color);
  let themeDark = $state(defaults.theme_color_dark);
  let background = $state(defaults.background_color);
  let icon = $state('');
  let images = $state<ImageInfo[]>([]);
  let error = $state('');
  let status = $state('');
  let busy = $state(false);
  // Changes with the icon, so the preview is drawn again.
  let drawn = $state(0);

  const swatch = 'h-11 w-14 rounded border-2 border-zinc-900 dark:border-zinc-400';
  const shownName =$derived(name.trim() || instanceName);
  const shownShort = $derived(shortName.trim() || shownName);

  function take(s: AppSettings) {
    instanceName = s.instance_name;
    [name, shortName, theme, themeDark, background, icon] = [s.name, s.short_name, s.theme_color, s.theme_color_dark, s.background_color, s.icon_image_id];
    installedApp.settings = s;
    drawn++;
  }

  onMount(async () => {
    try {
      take(await request<AppSettings>('GET', '/settings/app'));
      if (session.can('assets.read')) images = (await request<{ images: ImageInfo[] }>('GET', '/images')).images ?? [];
      loaded = true;
    } catch (err) {
      error = messageOf(err);
    }
  });

  async function save(event: SubmitEvent) {
    event.preventDefault();
    error = status = '';
    busy = true;
    try {
      take(
        await request<AppSettings>('PUT', '/settings/app', {
          name,
          short_name: shortName,
          theme_color: theme,
          theme_color_dark: themeDark,
          background_color: background,
          icon_image_id: icon,
        }),
      );
      status = 'Saved. Phones that installed it update their icon and name when they next check, which may take a day.';
    } catch (err) {
      error = messageOf(err);
    } finally {
      busy = false;
    }
  }

  async function upload(event: Event) {
    const input = event.currentTarget as HTMLInputElement;
    const file = input.files?.[0];
    if (!file) return;
    error = status = '';
    busy = true;
    try {
      const form = new FormData();
      form.append('file', file);
      const s = await request<AppSettings>('POST', '/settings/app/icon', form);
      take(s);
      if (session.can('assets.read')) images = (await request<{ images: ImageInfo[] }>('GET', '/images')).images ?? [];
      status = 'The logo is uploaded, added to the image library, and is now the icon.';
    } catch (err) {
      error = messageOf(err);
    } finally {
      busy = false;
      input.value = '';
    }
  }

  function resetColors() {
    [theme, themeDark, background] = [defaults.theme_color, defaults.theme_color_dark, defaults.background_color];
  }
</script>

<PageHeader title="Installed app" lede="People install BAM! on a phone or computer from the browser, as an app of its own. This is its name, icon and colors there." />

<ErrorMessage {error} />
{#if loaded}
  <form class="max-w-3xl space-y-6" onsubmit={save}>
    <section class="card space-y-3" aria-labelledby="app-name-heading">
      <h2 id="app-name-heading" class="text-lg font-semibold">Names</h2>
      <div class="grid gap-3 sm:grid-cols-2">
        <div>
          <label class="label" for="app-name">Name</label>
          <input id="app-name" class="input w-full" maxlength="100" placeholder={instanceName} bind:value={name} aria-describedby="app-name-hint" />
          <p id="app-name-hint" class="mt-1 text-xs text-zinc-600 dark:text-zinc-400">Left empty, the instance's name, as on PDFs: {instanceName}.</p>
        </div>
        <div>
          <label class="label" for="app-short-name">Short name</label>
          <input id="app-short-name" class="input w-full" maxlength="30" placeholder={shownName} bind:value={shortName} aria-describedby="app-short-hint" />
          <p id="app-short-hint" class="mt-1 text-xs text-zinc-600 dark:text-zinc-400">Under the icon, where about 12 characters fit. Left empty, the name.</p>
        </div>
      </div>
    </section>

    <section class="card space-y-3" aria-labelledby="app-icon-heading">
      <h2 id="app-icon-heading" class="text-lg font-semibold">Icon</h2>
      <div class="flex flex-wrap items-start gap-6">
        <figure class="space-y-1 text-center">
          <img
            src="/app-icons/icon-192.png?v={drawn}"
            alt="The icon"
            width="96"
            height="96"
            class="rounded-2xl border-2 border-zinc-900 dark:border-zinc-400"
          />
          <figcaption class="max-w-24 truncate text-xs">{shownShort}</figcaption>
        </figure>
        <div class="min-w-60 grow space-y-3">
          <div>
            <label class="label" for="app-icon">Drawn from</label>
            <select id="app-icon" class="input w-full" bind:value={icon}>
              <option value="">BAM!'s logo</option>
              {#each images as img (img.id)}
                <option value={img.id}>{img.filename}</option>
              {/each}
            </select>
            <p class="mt-1 text-xs text-zinc-600 dark:text-zinc-400">
              An image from the <a class="inline-flex min-h-11 items-center underline" href="/images">image library</a>, centered on the background color. Save to see it here.
            </p>
          </div>
          <div>
            <label class="label" for="app-icon-file">Or upload a logo</label>
            <input id="app-icon-file" type="file" accept="image/png,image/jpeg" class="input w-full" disabled={busy} onchange={upload} />
            <p class="mt-1 text-xs text-zinc-600 dark:text-zinc-400">A square PNG with room around it looks best. Anyone can see the icon, signed in or not.</p>
          </div>
        </div>
      </div>
    </section>

    <section class="card space-y-3" aria-labelledby="app-colors-heading">
      <h2 id="app-colors-heading" class="text-lg font-semibold">Colors</h2>
      <div class="flex flex-wrap gap-6">
        <div>
          <label class="label" for="app-theme">Bar above the app, light</label>
          <div class="flex items-center gap-2">
            <input id="app-theme" type="color" class={swatch} bind:value={theme} />
            <span class="font-mono text-sm">{theme}</span>
          </div>
        </div>
        <div>
          <label class="label" for="app-theme-dark">Bar above the app, dark</label>
          <div class="flex items-center gap-2">
            <input id="app-theme-dark" type="color" class={swatch} bind:value={themeDark} />
            <span class="font-mono text-sm">{themeDark}</span>
          </div>
        </div>
        <div>
          <label class="label" for="app-background">Behind the icon, and as it opens</label>
          <div class="flex items-center gap-2">
            <input id="app-background" type="color" class={swatch} bind:value={background} />
            <span class="font-mono text-sm">{background}</span>
          </div>
        </div>
      </div>
      <button type="button" class="btn-secondary" onclick={resetColors}>BAM!'s colors</button>
    </section>

    <div class="flex flex-wrap items-center gap-3">
      <button type="submit" class="btn-primary" disabled={busy}>Save</button>
      {#if status}<p role="status" class="text-sm text-green-700 dark:text-green-400">{status}</p>{/if}
    </div>
  </form>
{/if}
