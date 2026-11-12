<script lang="ts">
  // Reads codes with the camera (docs/scanning.md), calling onfound with
  // the text of the first one seen: with the browser's own code reader, or
  // where it has none, as on iOS, the one BAM! packages. Where the browser
  // has no camera to offer, it says so, and the code is typed instead.
  import { onDestroy, tick } from 'svelte';
  import { detectorClass, scanFormats, type Detector } from '../lib/codeReader';

  let {
    onfound,
    label = 'Scan with the camera',
    unsupported = 'This browser cannot read codes with the camera; type the code, or use a scanner that types it.',
  }: { onfound: (text: string) => void; label?: string; unsupported?: string } = $props();

  const canScan = Boolean(navigator.mediaDevices?.getUserMedia);

  let video: HTMLVideoElement | undefined = $state();
  let stream: MediaStream | null = null;
  let scanning = $state(false);
  // Between the press and the camera showing, which the first scan in a
  // browser without its own code reader spends fetching BAM!'s.
  let starting = $state(false);
  let cameraError = $state('');
  let frame = 0;

  onDestroy(stop);

  async function start() {
    if (starting) return;
    cameraError = '';
    starting = true;
    try {
      await open();
    } finally {
      starting = false;
    }
  }

  async function open() {
    try {
      stream = await navigator.mediaDevices.getUserMedia({ video: { facingMode: 'environment' } });
    } catch {
      stop();
      cameraError = 'The camera could not be opened. Allow this site to use it, or type the code instead.';
      return;
    }
    let detector: Detector;
    try {
      const DetectorType = await detectorClass();
      const supported = await DetectorType.getSupportedFormats();
      const formats = scanFormats.filter((f) => supported.includes(f));
      detector = new DetectorType({ formats: formats.length ? formats : [...supported] });
    } catch {
      stop();
      cameraError = 'The code reader could not be loaded. Try again with a connection, or type the code instead.';
      return;
    }
    if (!stream) return;
    scanning = true;
    await tick();
    if (!video) return;
    video.srcObject = stream;
    try {
      await video.play();
    } catch {
      stop();
      cameraError = 'The camera could not be opened. Allow this site to use it, or type the code instead.';
      return;
    }
    const look = async () => {
      if (!scanning || !video) return;
      try {
        const found = await detector.detect(video);
        if (found[0]?.rawValue) {
          stop();
          onfound(found[0].rawValue);
          return;
        }
      } catch {
        // A frame it could not read: try the next.
      }
      frame = requestAnimationFrame(look);
    };
    frame = requestAnimationFrame(look);
  }

  function stop() {
    scanning = false;
    cancelAnimationFrame(frame);
    stream?.getTracks().forEach((t) => t.stop());
    stream = null;
  }
</script>

{#if canScan}
  <div class="space-y-3">
    {#if scanning}
      <!-- svelte-ignore a11y_media_has_caption -->
      <video bind:this={video} class="w-full rounded bg-black" playsinline muted></video>
      <button type="button" class="btn-secondary" onclick={stop}>Stop</button>
    {:else}
      <button type="button" class="btn-secondary" onclick={start} disabled={starting}>{starting ? 'Starting the camera…' : label}</button>
    {/if}
    {#if cameraError}<p class="error-text">{cameraError}</p>{/if}
  </div>
{:else}
  <p class="text-sm text-zinc-600 dark:text-zinc-400">{unsupported}</p>
{/if}
