export type RunStatus = "success" | "nochange" | "failed" | "skipped" | "running";
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
  fields: ProviderField[];
}

export interface Provider {
  id: string;
  name: string;
  type: string;
  enabled: boolean;
  status: "ok" | "warn" | "err";
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
  type: "A" | "AAAA" | "CNAME";
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
  lastStatus: RunStatus;
  nextRun: number | null;
  current: string;
  desired: string;
  lastChanged: number | null;
  error?: string;
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
}

export interface IPSource {
  id: string;
  name: string;
  type: string;
  family: "v4" | "v6";
  target: string;
  enabled: boolean;
}

export interface AppData {
  providerTypes: ProviderType[];
  ipSourceTypes: any[];
  ipSources: IPSource[];
  interfaces: any[];
  providers: Provider[];
  jobs: Job[];
  runs: Run[];
  ipInfo: any;
  runtime: any;
  scheduler: any;
  settings: any;
  activity: any[];
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
