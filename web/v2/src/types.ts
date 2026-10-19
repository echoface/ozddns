export type RunStatus = "success" | "nochange" | "failed" | "skipped" | "running";
export type JobStatus = RunStatus | "not_run";
export type ThemePreference = "light" | "dark" | "auto";

export interface ProviderField {
  key: string;
  label: string;
  type: string;
  required?: boolean;
  help?: string;
  default?: unknown;
  options?: string[];
  mono?: boolean;
}

export interface ProviderType {
  id: string;
  label: string;
  color: string;
  tag: string;
  desc: string;
  supportsProxied: boolean;
  // writeOnly providers cannot be read back (DuckDNS, Namecheap DDNS, custom
  // webhooks); OzDDNS tracks the last value it wrote instead.
  writeOnly?: boolean;
  fields: ProviderField[];
}

export interface Provider {
  id: string;
  name: string;
  type: string;
  enabled: boolean;
  status: "ok" | "warn" | "err" | "mute";
  statusText: string;
  zones: string[];
  records: number;
  config: Record<string, unknown>;
  lastValidated: number | null;
  createdAt: number | null;
  updatedAt?: number | null;
}

export interface Target {
  domain: string;
  type: string;
  ttl: number;
  proxied?: boolean;
  comment?: string;
}

export interface Job {
  id: string;
  name: string;
  providerId: string;
  ipSourceId: string;
  targets: Target[];
  intervalMinutes: number;
  runOnChangeOnly: boolean;
  jitter: boolean;
  enabled: boolean;
  lastRun: number | null;
  lastStatus: JobStatus;
  nextRun: number | null;
  current: string;
  desired: string;
  lastChanged: number | null;
  error?: string;
  note?: string;
}

// "applied" is a write to a provider that cannot be read back (DuckDNS,
// Namecheap DDNS, custom webhooks): the address was pushed, but there is no
// read API to diff it against.
export type RunAction = "created" | "updated" | "applied" | "unchanged" | "skipped" | "failed";

export interface RunDetail {
  domain: string;
  type: string;
  action: RunAction;
  current?: string;
  desired?: string;
  proxied?: boolean;
  note?: string;
}

export interface Run {
  id: string;
  jobId: string;
  startedAt: number;
  durationMs: number;
  status: RunStatus;
  oldIp: string | null;
  newIp: string | null;
  trigger: "manual" | "schedule";
  message: string;
  records: number;
  details?: RunDetail[];
}

export interface IPSource {
  id: string;
  name: string;
  type: string;
  family: "v4" | "v6";
  target: string;
}

export type IPFamilyStatus = "ok" | "error" | "no_source";

export interface IPFamilyInfo {
  address: string;
  sourceId: string;
  source: string;
  sourceTarget: string;
  status: IPFamilyStatus;
  error: string;
  checkedAt: number | null;
}

export interface IPInfo {
  v4: IPFamilyInfo;
  v6: IPFamilyInfo;
  checkedAt: number | null;
}

export interface AppData {
  providerTypes: ProviderType[];
  ipSourceTypes: any[];
  ipSources: IPSource[];
  interfaces: any[];
  providers: Provider[];
  jobs: Job[];
  runs: Run[];
  ipInfo: IPInfo;
  runtime: any;
  scheduler: any;
  settings: any;
  activity: any[];
}

export interface AuthState {
  enabled: boolean;
  authenticated: boolean;
  setupPending: boolean;
  username?: string;
  recoveryCodesRemaining: number;
  sessions: number;
  listen: string;
  exposed: boolean;
}

export interface AuthSetupInfo {
  secret: string;
  otpauthUrl: string;
  qrCodeDataUrl: string;
  expiresAt: string;
}

export interface AuthSetupResult {
  recoveryCodes: string[];
  user: { username: string };
}

export interface AuthEvent {
  id: number;
  at: string;
  event: string;
  username?: string;
  detail?: string;
  ip?: string;
  userAgent?: string;
}

export interface Toast {
  id: number;
  kind: "ok" | "err" | "info";
  title: string;
  desc: string;
}

export interface ModalState {
  type: string;
  [key: string]: any;
}
