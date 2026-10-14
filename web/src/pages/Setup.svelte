<script lang="ts">
  import AuthLayout from '../components/AuthLayout.svelte';
  import { api } from '../lib/api';
  import { session } from '../lib/session.svelte';
  import { go } from '../lib/router.svelte';

  let token = $state('');
  let username = $state('');
  let email = $state('');
  let password = $state('');
  let password2 = $state('');
  let panelName = $state('Hearth');
  let panelUrl = $state(location.origin);
  let error = $state('');
  let busy = $state(false);

  async function submit(e: Event) {
    e.preventDefault();
    error = '';
    if (password !== password2) {
      error = 'Die Passwörter stimmen nicht überein.';
      return;
    }
    busy = true;
    try {
      const r = await api.post('/api/setup', { token, username, email, password, panelName, panelUrl });
      session.needsSetup = false;
      session.setUser(r.user);
      session.panel.name = panelName;
      await session.loadMeta();
      go('/', true);
    } catch (err: any) {
      error = err.message;
    } finally {
      busy = false;
    }
  }
</script>

<AuthLayout title="Panel einrichten" subtitle="Lege das erste Administrator-Konto an. Danach kannst du weitere Benutzer einladen." wide>
  <form class="stack" onsubmit={submit}>
    <div class="field">
      <label for="tok">Setup-Code</label>
      <input id="tok" class="input mono" bind:value={token} placeholder="ABCD-EFGH" required autocomplete="off" />
      <p class="hint">Steht in der Konsole bzw. im Log beim ersten Start und in der Datei <code>setup-code.txt</code> im Datenverzeichnis.</p>
    </div>
    <div class="form-grid">
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
      </div>
      <div class="field">
        <label for="p2">Passwort wiederholen</label>
        <input id="p2" class="input" type="password" bind:value={password2} required autocomplete="new-password" />
      </div>
    </div>
    <hr class="sep" />
    <div class="form-grid">
      <div class="field">
        <label for="pn">Name des Panels</label>
        <input id="pn" class="input" bind:value={panelName} maxlength="60" />
      </div>
      <div class="field">
        <label for="pu">Öffentliche Adresse</label>
        <input id="pu" class="input" bind:value={panelUrl} />
      </div>
    </div>
    <p class="hint">
      Die E-Mail wird als Kontakt an Download-Dienste (PaperMC, Modrinth) übermittelt, wie es deren Nutzungsbedingungen verlangen. Die Adresse wird für Einladungslinks und
      öffentliche Serverseiten verwendet.
    </p>
    {#if error}<p class="error-text">{error}</p>{/if}
    <button class="btn primary" type="submit" disabled={busy}>Konto anlegen und starten</button>
  </form>
</AuthLayout>
