<script lang="ts">
  import AuthLayout from '../components/AuthLayout.svelte';
  import { api } from '../lib/api';
  import { session } from '../lib/session.svelte';
  import { go, router } from '../lib/router.svelte';

  let username = $state('');
  let password = $state('');
  let code = $state('');
  let step = $state<'password' | 'code'>(session.twoFactor ? 'code' : 'password');
  let error = $state('');
  let busy = $state(false);

  async function done(user: any) {
    session.setUser(user);
    await session.loadMeta();
    const next = router.route.query.get('next');
    go(next && next.startsWith('/') && !next.startsWith('//') ? next : '/', true);
  }

  async function submit(e: Event) {
    e.preventDefault();
    error = '';
    busy = true;
    try {
      if (step === 'password') {
        const r = await api.post('/api/auth/login', { username, password });
        if (r.twoFactor) {
          step = 'code';
        } else {
          await done(r.user);
        }
      } else {
        const r = await api.post('/api/auth/2fa', { code });
        await done(r.user);
      }
    } catch (err: any) {
      error = err.message;
      if (err.status === 429 && step === 'code') step = 'password';
    } finally {
      busy = false;
    }
  }
</script>

<AuthLayout title={step === 'password' ? 'Anmelden' : 'Bestätigungscode'} subtitle={step === 'code' ? 'Gib den 6-stelligen Code aus deiner Authenticator-App oder einen Wiederherstellungscode ein.' : ''}>
  <form class="stack" onsubmit={submit}>
    {#if step === 'password'}
      <div class="field">
        <label for="u">Benutzername</label>
        <input id="u" class="input" bind:value={username} autocomplete="username" required />
      </div>
      <div class="field">
        <label for="p">Passwort</label>
        <input id="p" class="input" type="password" bind:value={password} autocomplete="current-password" required />
      </div>
    {:else}
      <div class="field">
        <label for="c">Code</label>
        <input id="c" class="input mono code" bind:value={code} inputmode="numeric" autocomplete="one-time-code" placeholder="123456" required />
      </div>
    {/if}
    {#if error}<p class="error-text">{error}</p>{/if}
    <button class="btn primary" type="submit" disabled={busy}>{step === 'password' ? 'Anmelden' : 'Bestätigen'}</button>
    {#if step === 'code'}
      <button type="button" class="btn ghost" onclick={() => ((step = 'password'), (code = ''))}>Zurück</button>
    {/if}
  </form>
</AuthLayout>

<style>
  .code {
    font-size: 20px;
    letter-spacing: 0.3em;
    height: 46px;
    text-align: center;
  }
</style>
