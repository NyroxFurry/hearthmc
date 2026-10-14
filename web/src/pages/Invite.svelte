<script lang="ts">
  import { onMount } from 'svelte';
  import AuthLayout from '../components/AuthLayout.svelte';
  import { api } from '../lib/api';
  import { session } from '../lib/session.svelte';
  import { go } from '../lib/router.svelte';

  let { token }: { token: string } = $props();
  let info = $state<{ serverName?: string; expiresAt: number } | null>(null);
  let invalid = $state('');
  let username = $state('');
  let email = $state('');
  let password = $state('');
  let error = $state('');
  let busy = $state(false);

  onMount(async () => {
    try {
      info = await api.get(`/api/invites/${encodeURIComponent(token)}`);
    } catch (e: any) {
      invalid = e.message;
    }
  });

  async function submit(e: Event) {
    e.preventDefault();
    busy = true;
    error = '';
    try {
      const r = await api.post(`/api/invites/${encodeURIComponent(token)}`, { username, email, password });
      session.setUser(r.user);
      await session.loadMeta();
      go('/', true);
    } catch (err: any) {
      error = err.message;
    } finally {
      busy = false;
    }
  }
</script>

<AuthLayout
  title="Einladung annehmen"
  subtitle={info?.serverName ? `Du wurdest zum Server „${info.serverName}“ eingeladen.` : 'Lege dein Konto an.'}
>
  {#if invalid}
    <div class="notice danger"><span>{invalid}</span></div>
    <a class="btn" href="/login" style="margin-top:16px">Zur Anmeldung</a>
  {:else if info}
    <form class="stack" onsubmit={submit}>
      <div class="field">
        <label for="u">Benutzername</label>
        <input id="u" class="input" bind:value={username} required autocomplete="username" />
      </div>
      <div class="field">
        <label for="m">E-Mail <span class="faint">(optional)</span></label>
        <input id="m" class="input" type="email" bind:value={email} autocomplete="email" />
      </div>
      <div class="field">
        <label for="p">Passwort</label>
        <input id="p" class="input" type="password" bind:value={password} minlength="8" required autocomplete="new-password" />
        <p class="hint">Mindestens 8 Zeichen.</p>
      </div>
      {#if error}<p class="error-text">{error}</p>{/if}
      <button class="btn primary" disabled={busy}>Konto anlegen</button>
    </form>
  {/if}
</AuthLayout>
