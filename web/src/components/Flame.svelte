<script lang="ts">
  // Hearth-Zeichen als Linienzeichnung: Kontur mit 2,5er Strich, ohne Füllung.
  let { size = 26 }: { size?: number } = $props();
</script>

<svg width={size} height={size} viewBox="0 0 40 40" fill="none" aria-hidden="true">
  <path
    d="M20 3C21.6 10 33 14 33 25a13 13 0 0 1-26 0c0-4.6 2-8.2 5-10.8.5 3.2 1.9 5.4 4 6.6C15.4 14.6 16.6 8.6 20 3Z"
    stroke="currentColor"
    stroke-width="2.5"
    stroke-linejoin="round"
  />
  <path d="M14 26.5a6 6 0 0 0 6 6" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" />
</svg>
