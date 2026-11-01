<script lang="ts">
  // Reads codes with the camera, where the browser can (docs/scanning.md),
  // calling onfound with the text of the first one seen. Where it cannot,
  // it says so, and the code is typed instead.
  import { onDestroy, tick } from 'svelte';

  let {
    onfound,
    label = 'Scan with the camera',
    unsupported = 'This browser cannot read codes with the camera; type the code, or use a scanner that types it.',
  }: { onfound: (text: string) => void; label?: string; unsupported?: string } = $props();

  interface Detected {
    rawValue: string;
  }
  interface Detector {
    detect(source: HTMLVideoElement): Promise<Detected[]>;
  }
  interface DetectorClass {
    new (options: { formats: string[] }): Detector;
    getSupportedFormats(): Promise<string[]>;
  }

  const DetectorType = (globalThis as { BarcodeDetector?: DetectorClass }).BarcodeDetector;
  const canScan = Boolean(DetectorType && navigator.mediaDevices?.getUserMedia);

  let video: HTMLVideoElement | undefined = $state();
  let stream: MediaStream | null = null;
  let scanning = $state(false);
  let cameraError = $state('');
  let frame = 0;

  onDestroy(stop);

  async function start() {
    if (!DetectorType) return;
    cameraError = '';
    try {
      const supported = await DetectorType.getSupportedFormats();
      const formats = ['qr_code', 'micro_qr_code', 'code_128'].filter((f) => supported.includes(f));
      const detector = new DetectorType({ formats: formats.length ? formats : supported });
      stream = await navigator.mediaDevices.getUserMedia({ video: { facingMode: 'environment' } });
      scanning = true;
      await tick();
      if (!video) return;
      video.srcObject = stream;
      await video.play();
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
    } catch {
      stop();
      cameraError = 'The camera could not be opened. Allow this site to use it, or type the code instead.';
    }
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
      <button type="button" class="btn-secondary" onclick={start}>{label}</button>
    {/if}
    {#if cameraError}<p class="error-text">{cameraError}</p>{/if}
  </div>
{:else}
  <p class="text-sm text-zinc-600 dark:text-zinc-400">{unsupported}</p>
{/if}
