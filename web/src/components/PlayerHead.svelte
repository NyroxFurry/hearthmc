<script lang="ts">
  let { uuid, name = '', size = 24, base = '/api/heads' }: { uuid: string; name?: string; size?: number; base?: string } = $props();
  let px = $derived(size <= 32 ? 32 : size <= 64 ? 64 : 128);
  // players without a known UUID still get a stable default face
  let key = $derived(uuid || (name ? 'name-' + name.toLowerCase() : 'steve'));
</script>

<img
  class="head-img"
  src="{base}/{encodeURIComponent(key)}?size={px}"
  width={size}
  height={size}
  alt={name ? `Kopf von ${name}` : ''}
  loading="lazy"
/>
