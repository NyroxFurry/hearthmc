// German descriptions of audit log entries.
import type { AuditEntry } from './types';

const labels: Record<string, string> = {
  'auth.login': 'Angemeldet',
  'auth.login_failed': 'Anmeldung fehlgeschlagen',
  'auth.logout': 'Abgemeldet',
  'account.2fa_enabled': 'Zwei-Faktor-Anmeldung eingeschaltet',
  'account.2fa_disabled': 'Zwei-Faktor-Anmeldung ausgeschaltet',
  'account.password': 'Passwort geändert',
  'account.apikey_created': 'API-Schlüssel erstellt',
  'panel.setup': 'Panel eingerichtet',
  'user.register': 'Konto über Einladung angelegt',
  'server.create': 'Server angelegt',
  'server.update': 'Einstellungen geändert',
  'server.delete': 'Server gelöscht',
  'server.power': 'Energieaktion',
  'server.command': 'Befehl gesendet',
  'server.reinstall': 'Neu installiert',
  'files.write': 'Datei gespeichert',
  'files.upload': 'Dateien hochgeladen',
  'files.mkdir': 'Ordner angelegt',
  'files.touch': 'Datei angelegt',
  'files.rename': 'Umbenannt/verschoben',
  'files.copy': 'Kopiert',
  'files.delete': 'Gelöscht',
  'files.compress': 'Archiv erstellt',
  'files.extract': 'Archiv entpackt',
  'backup.create': 'Backup erstellt',
  'backup.restore': 'Backup wiederhergestellt',
  'backup.delete': 'Backup gelöscht',
  'backup.settings': 'Backup-Ausnahmen geändert',
  'schedule.create': 'Zeitplan angelegt',
  'schedule.update': 'Zeitplan geändert',
  'schedule.delete': 'Zeitplan gelöscht',
  'schedule.run': 'Zeitplan ausgeführt',
  'member.add': 'Mitglied hinzugefügt',
  'member.update': 'Rechte geändert',
  'member.remove': 'Mitglied entfernt',
  'invite.create': 'Einladung erstellt',
  'invite.accept': 'Einladung angenommen',
  'webhook.save': 'Benachrichtigung gespeichert',
  'public.update': 'Öffentliche Seite geändert',
  'mc.properties': 'server.properties geändert',
  'mc.action': 'Spieleraktion',
  'mc.version': 'Version gewechselt',
  'mc.icon': 'Servericon geändert',
  'mc.world_reset': 'Welt zurückgesetzt',
  'mc.plugin_install': 'Erweiterung installiert',
  'mc.plugin_toggle': 'Erweiterung umgeschaltet',
  'mc.plugin_delete': 'Erweiterung gelöscht',
  'map.render': 'Karte gerendert',
  'map.settings': 'Karteneinstellungen geändert',
  'map.plugin': 'Karten-Plugin geändert',
  'user.create': 'Benutzer angelegt',
  'user.update': 'Benutzer geändert',
  'user.delete': 'Benutzer gelöscht',
  'user.reset_2fa': '2FA zurückgesetzt',
  'settings.update': 'Panel-Einstellungen geändert',
  'java.install': 'Java installiert',
  'java.remove': 'Java entfernt',
};

const powerLabels: Record<string, string> = { start: 'Gestartet', stop: 'Gestoppt', restart: 'Neu gestartet', kill: 'Hart beendet' };
const mcActions: Record<string, string> = {
  kick: 'gekickt',
  ban: 'gesperrt',
  pardon: 'entsperrt',
  op: 'zum Operator gemacht',
  deop: 'Operator entzogen',
  whitelist_add: 'zur Whitelist hinzugefügt',
  whitelist_remove: 'von der Whitelist entfernt',
  ban_ip: 'IP gesperrt',
  pardon_ip: 'IP entsperrt',
  whitelist_on: 'Whitelist eingeschaltet',
  whitelist_off: 'Whitelist ausgeschaltet',
};

export function auditTitle(e: AuditEntry): string {
  const d = e.details ?? {};
  if (e.action === 'server.power' && d.action) return powerLabels[d.action] ?? d.action;
  if (e.action === 'mc.action' && d.action) {
    const who = d.player || d.ip || '';
    return who ? `${who} ${mcActions[d.action] ?? d.action}` : (mcActions[d.action] ?? d.action);
  }
  return labels[e.action] ?? e.action;
}

export function auditDetail(e: AuditEntry): string {
  const d = e.details;
  if (!d || typeof d !== 'object') return '';
  switch (e.action) {
    case 'server.command':
      return d.command ?? '';
    case 'files.write':
    case 'files.mkdir':
    case 'files.touch':
      return d.path ?? '';
    case 'files.rename':
    case 'files.copy':
      return `${d.from} → ${d.to}`;
    case 'files.delete':
    case 'files.compress':
      return (d.paths ?? []).join(', ');
    case 'files.upload':
      return (d.files ?? []).slice(0, 5).join(', ') + ((d.files?.length ?? 0) > 5 ? ` und ${d.files.length - 5} weitere` : '');
    case 'files.extract':
      return d.path ?? '';
    case 'server.update':
      return (d.fields ?? []).join(', ');
    case 'mc.properties':
      return (d.keys ?? []).join(', ');
    case 'mc.version':
      return `${d.flavor} ${d.version}${d.build ? ' (' + d.build + ')' : ''}`;
    case 'mc.plugin_install':
      return (d.files ?? []).join(', ');
    case 'mc.plugin_toggle':
    case 'mc.plugin_delete':
      return d.file ?? '';
    case 'map.plugin':
      return ({ bluemap: 'BlueMap', squaremap: 'squaremap', dynmap: 'Dynmap' } as Record<string, string>)[d.plugin] ?? (d.plugin ? d.plugin : 'ausgeschaltet');
    case 'member.add':
    case 'member.update':
    case 'member.remove':
      return d.user ?? '';
    case 'backup.create':
    case 'backup.restore':
    case 'backup.delete':
    case 'schedule.create':
    case 'schedule.update':
    case 'schedule.delete':
    case 'webhook.save':
    case 'server.create':
    case 'server.delete':
      return d.name ?? '';
    case 'mc.action':
      return d.reason ?? (d.offline ? 'bei gestopptem Server gespeichert' : '');
    case 'schedule.run':
      return [d.schedule, d.status === 'ok' ? '' : d.error || d.status].filter(Boolean).join(': ');
    case 'public.update':
      return d.enabled ? `veröffentlicht unter /s/${d.slug}` : 'nicht öffentlich';
    case 'user.create':
    case 'user.update':
    case 'user.delete':
      return d.username ?? d.user ?? '';
  }
  return '';
}
