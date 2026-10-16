// Types mirroring the JSON returned by the Go API.

export interface User {
  id: number;
  username: string;
  email: string;
  isAdmin: boolean;
  disabled: boolean;
  canCreateServers: boolean;
  maxServers: number;
  maxMemoryMb: number;
  totpEnabled: boolean;
  prefs: Record<string, unknown>;
  createdAt: number;
  updatedAt: number;
  lastLoginAt: number;
  servers?: number;
}

export interface PanelInfo {
  name: string;
  version: string;
  publicUrl: string;
}

export type State = 'offline' | 'installing' | 'starting' | 'running' | 'stopping';

export interface Status {
  state: State;
  startedAt: number;
  cpu: number;
  mem: number;
  memLimit: number;
  disk: number;
  players: number;
  maxPlayers: number;
  busy?: string;
  installStep?: string;
  lastExitCode: number;
  lastCrashAt: number;
  crashLoop?: boolean;
  pid?: string;
  /** Java-Server: genutzter Heap und Heap-Größe in Bytes */
  heap?: number;
  heapSize?: number;
}

export interface PortSpec {
  port: number;
  protocol: 'tcp' | 'udp' | 'both';
  note?: string;
}

export interface MapSettings {
  enabled: boolean;
  dimensions: string[];
  intervalMinutes: number;
  externalUrl: string;
  livePlayers: boolean;
  plugin?: string;
  pluginPort?: number;
}

export interface MapPluginInfo {
  id: string;
  name: string;
  project: string;
  description: string;
  flavors: string[];
}

export interface MCSettings {
  flavor: string;
  version: string;
  build: string;
  jar: string;
  java: string;
  javaMajor: number;
  flagsPreset: string;
  jvmArgs: string;
  serverArgs: string;
  minMemoryMb: number;
  rconPort: number;
  manageRcon: boolean;
  eula: boolean;
  trackChat: boolean;
  startupOverride: string;
  map: MapSettings;
  backupIgnore?: string[];
  installedVersion: string;
  installedBuild: string;
  loaderArgsFile: string;
}

export interface GenericSettings {
  command: string;
  stopCommand: string;
  readyRegex: string;
  stopTimeout: number;
  env: string[];
}

export interface Server {
  id: string;
  name: string;
  description: string;
  game: string;
  ownerId: number;
  runtime: 'native' | 'docker';
  memoryMb: number;
  cpuLimit: number;
  diskLimitMb: number;
  port: number;
  extraPorts: PortSpec[];
  autostart: boolean;
  autoRestart: boolean;
  dockerImage: string;
  settings: any;
  installState: string;
  installError: string;
  publicEnabled: boolean;
  publicSlug: string;
  publicConfig: any;
  displayAddress: string;
  color: string;
  suspended: boolean;
  createdAt: number;
  updatedAt: number;
  status: Status;
  permissions: string[];
  isOwner: boolean;
  ownerName: string;
  gameName: string;
  publicUrl?: string;
  hasIcon?: boolean;
}

export interface PermInfo {
  key: string;
  group: string;
  label: string;
  description: string;
}

export interface Preset {
  id: string;
  label: string;
  description: string;
  permissions: string[];
}

export interface Flavor {
  id: string;
  name: string;
  description: string;
  kind: 'vanilla' | 'plugins' | 'mods' | 'custom';
}

export interface Meta {
  games: { id: string; name: string; adminOnly: boolean }[];
  flavors: Flavor[];
  permissions: PermInfo[];
  presets: Preset[];
  webhookEvents: { key: string; label: string }[];
  docker: boolean;
  defaultRuntime: string;
  defaultMemory: number;
  portRange: number[];
  deathCauses: Record<string, string>;
  mapPlugins: MapPluginInfo[];
}

export interface FileEntry {
  name: string;
  path: string;
  isDir: boolean;
  size: number;
  modTime: number;
  mode: string;
  symlink?: boolean;
  editable: boolean;
  archive?: boolean;
}

export interface Backup {
  id: string;
  serverId: string;
  name: string;
  size: number;
  files: number;
  status: 'running' | 'ok' | 'failed';
  error: string;
  locked: boolean;
  createdBy: number;
  createdAt: number;
  finishedAt: number;
}

export interface ScheduleStep {
  type: 'command' | 'say' | 'power' | 'backup' | 'wait';
  value: string;
  seconds?: number;
}

export interface Schedule {
  id: number;
  serverId: string;
  name: string;
  cron: string;
  enabled: boolean;
  onlyWhenOnline: boolean;
  steps: ScheduleStep[];
  lastRunAt: number;
  lastStatus: string;
  lastError: string;
  createdAt: number;
  next: number[];
  running: boolean;
}

export interface OnlinePlayer {
  uuid: string;
  name: string;
  since: number;
}

export interface MCPlayer {
  uuid: string;
  name: string;
  firstSeen: number;
  lastSeen: number;
  trackedSeconds: number;
  sessions: number;
  playTicks: number;
  deaths: number;
  mobKills: number;
  playerKills: number;
  blocksMined: number;
  distanceCm: number;
  jumps: number;
  damageDealt: number;
  damageTaken: number;
  itemsCrafted: number;
  fishCaught: number;
  advancements: number;
  online?: boolean;
  op?: boolean;
  playSeconds: number;
}

export interface MCEvent {
  id: number;
  at: number;
  type: 'death' | 'advancement' | 'chat' | 'join' | 'leave';
  uuid: string;
  name: string;
  message: string;
  data: any;
}

export interface Member {
  serverId: string;
  userId: number;
  username: string;
  permissions: string[];
  addedBy: number;
  createdAt: number;
}

export interface AuditEntry {
  id: number;
  at: number;
  userId: number;
  username: string;
  serverId: string;
  action: string;
  details: any;
  ip: string;
}

export interface Webhook {
  id: number;
  serverId: string;
  name: string;
  kind: 'discord' | 'generic';
  url: string;
  events: string[];
  enabled: boolean;
  lastStatus: string;
  createdAt: number;
}

export interface Position {
  uuid: string;
  name: string;
  x: number;
  y: number;
  z: number;
  dim: string;
  at: number;
  live: boolean;
}

export interface DimInfo {
  key: string;
  minZoom: number;
  minX: number;
  minZ: number;
  maxX: number;
  maxZ: number;
  regions: number;
  lastRender: number;
}

export interface MapInfo {
  settings: MapSettings;
  dimensions: { key: string; id: string; label: string; info: DimInfo }[];
  render?: { running: boolean; dim: string; done: number; total: number; rendered: number; startedAt: number; finishedAt: number; error?: string };
  spawn: { x: number; z: number };
  showPlayers?: boolean;
  /** what the renderer finds on disk */
  world?: { name: string; exists: boolean; layout: string; dimensions: { key: string; id: string; label: string; regions: number }[] };
  running?: boolean;
  /** map plugin installed by the panel (BlueMap, squaremap, Dynmap) */
  plugin?: { id: string; name: string; port?: number; file?: string; enabled?: boolean; supported?: boolean; url?: string; listening?: boolean };
  mapPort?: { url: string; port?: string; error: string; custom: boolean; disabled?: boolean; mixed: boolean };
}

export interface InstalledPlugin {
  file: string;
  enabled: boolean;
  size: number;
  modTime: number;
  id: string;
  name: string;
  version: string;
  description: string;
  authors: string[];
  website: string;
  loader: string;
  projectId?: string;
  slug?: string;
  iconUrl?: string;
  versionId?: string;
  update?: { versionId: string; versionNumber: string };
}

export interface MRHit {
  project_id: string;
  slug: string;
  title: string;
  description: string;
  icon_url: string;
  author: string;
  downloads: number;
  follows: number;
  categories: string[];
  versions: string[];
  project_type: string;
  date_modified: string;
}

export interface PlayerDetail {
  player: MCPlayer;
  playSeconds: number;
  events: MCEvent[];
  sessions?: { joinedAt: number; leftAt: number }[];
  deathCauses: Record<string, number>;
  causeLabels: Record<string, string>;
  daily: { day: string; seconds: number }[];
  stats?: {
    custom: Record<string, number>;
    mined: { id: string; value: number }[];
    killed: { id: string; value: number }[];
    killedBy: { id: string; value: number }[];
    crafted: { id: string; value: number }[];
    used: { id: string; value: number }[];
    broken: { id: string; value: number }[];
    pickedUp: { id: string; value: number }[];
  };
  online?: boolean;
  onlineSince?: number;
}
