/* ============================================================
   OzDDNS — Mock dataset + case catalog
   This is the "design + test data" seed. It maps 1:1 to the
   domain model that the Go backend will expose over REST.
   ============================================================ */
(function () {
  var MIN = 60 * 1000, HOUR = 60 * MIN, DAY = 24 * HOUR;
  var NOW = Date.now();
  function at(minsAgo) { return NOW - minsAgo * MIN; }

  /* ---- Provider type catalog (the "registry" contract) ---- */
  var providerTypes = [
    { id: 'cloudflare', label: 'Cloudflare', color: 'cloudflare', tag: 'cloudflare',
      desc: 'API Token, zones & proxied records (A / AAAA / CNAME)',
      supportsProxied: true, fields: [
        { key: 'api_token', label: 'API Token', type: 'secret', required: true, help: 'Zone.DNS Edit permission for the target zone.' },
        { key: 'zone_id', label: 'Zone ID (optional)', type: 'text', help: 'Leave blank to auto-discover from the domain.' },
        { key: 'proxied', label: 'Proxy traffic (orange cloud)', type: 'bool', default: false },
        { key: 'ttl', label: 'TTL (seconds)', type: 'number', default: 300 }
      ] },
    { id: 'alidns', label: 'Alibaba Cloud DNS', color: 'ali', tag: 'ali',
      desc: 'AccessKey pair, AliDNS / AliESA zones',
      supportsProxied: false, fields: [
        { key: 'access_key_id', label: 'AccessKey ID', type: 'text', required: true },
        { key: 'access_key_secret', label: 'AccessKey Secret', type: 'secret', required: true },
        { key: 'region', label: 'Region', type: 'select', options: ['cn-hangzhou', 'cn-beijing', 'ap-southeast-1'], default: 'cn-hangzhou' }
      ] },
    { id: 'dnspod', label: 'DNSPod (Tencent)', color: 'dnspod', tag: 'dnspod',
      desc: 'DNSPod token; also powers Tencent Cloud DNS',
      supportsProxied: false, fields: [
        { key: 'login_token_id', label: 'Token ID', type: 'text', required: true },
        { key: 'login_token', label: 'Token', type: 'secret', required: true },
        { key: 'record_line', label: 'Record line', type: 'text', default: '默认' }
      ] },
    { id: 'godaddy', label: 'GoDaddy', color: 'godaddy', tag: 'godaddy',
      desc: 'Production API key + secret',
      supportsProxied: false, fields: [
        { key: 'key', label: 'API Key', type: 'secret', required: true },
        { key: 'secret', label: 'API Secret', type: 'secret', required: true }
      ] },
    { id: 'namecheap', label: 'Namecheap', color: 'namecheap', tag: 'namecheap',
      desc: 'DDNS password from Advanced DNS',
      supportsProxied: false, fields: [
        { key: 'password', label: 'DDNS Password', type: 'secret', required: true },
        { key: 'domain', label: 'Domain', type: 'text', required: true, help: 'e.g. home.example.com' }
      ] },
    { id: 'duckdns', label: 'DuckDNS', color: 'duckdns', tag: 'duckdns',
      desc: 'Token + subdomain, free dynamic DNS',
      supportsProxied: false, fields: [
        { key: 'token', label: 'Token', type: 'secret', required: true },
        { key: 'subdomain', label: 'Subdomain', type: 'text', required: true, help: 'The name before .duckdns.org' }
      ] },
    { id: 'desec', label: 'deSEC', color: 'desec', tag: 'desec',
      desc: 'Token-based, DNSSEC-ready',
      supportsProxied: false, fields: [
        { key: 'token', label: 'API Token', type: 'secret', required: true }
      ] },
    { id: 'vercel', label: 'Vercel DNS', color: 'vercel', tag: 'vercel',
      desc: 'Project-scoped DNS records',
      supportsProxied: false, fields: [
        { key: 'token', label: 'Access Token', type: 'secret', required: true },
        { key: 'team_id', label: 'Team ID (optional)', type: 'text' }
      ] },
    { id: 'custom', label: 'Custom Webhook', color: 'custom', tag: 'custom',
      desc: 'Call your own HTTP endpoint with the new IP',
      supportsProxied: false, fields: [
        { key: 'url', label: 'Endpoint URL', type: 'text', required: true, help: 'Supports {{ip}} and {{domain}} placeholders.' },
        { key: 'method', label: 'Method', type: 'select', options: ['GET', 'POST', 'PUT'], default: 'GET' },
        { key: 'body', label: 'Body template', type: 'textarea', default: '{"domain":"{{domain}}","ip":"{{ip}}"}' },
        { key: 'auth', label: 'Authorization header', type: 'secret' }
      ] }
  ];

  var ipSourceTypes = [
    { id: 'http', label: 'HTTP lookup', icon: 'globe', desc: 'Query a public "what is my IP" endpoint.' },
    { id: 'interface', label: 'Network interface', icon: 'network', desc: 'Read the address bound to a local NIC (LAN / WAN).' },
    { id: 'command', label: 'Shell command', icon: 'terminal', desc: 'Run a command and extract the first matching IP.' },
    { id: 'static', label: 'Static / manual', icon: 'map-pin', desc: 'Pin a fixed address (testing or fixed-line).' }
  ];

  /* ---- IP sources ---- */
  var ipSources = [
    { id: 'src-ipify-v4', name: 'ipify (IPv4)', type: 'http', family: 'v4', target: 'https://api.ipify.org', enabled: true },
    { id: 'src-ifconfig-v6', name: 'ifconfig.co (IPv6)', type: 'http', family: 'v6', target: 'https://v6.ifconfig.co', enabled: true },
    { id: 'src-en0', name: 'LAN interface · en0', type: 'interface', family: 'v4', target: 'en0', enabled: true },
    { id: 'src-cmd-v6', name: 'Tailscale address', type: 'command', family: 'v6', target: 'tailscale ip -6', enabled: true }
  ];

  /* ---- Local NICs (offline sample; the live server enumerates real ones) ---- */
  var interfaces = [
    { name: 'en0', up: true, addresses: [
      { target: 'en0', name: 'en0', addr: '192.168.1.23', family: 'v4', index: 0, prefix: 24 },
      { target: 'en0#1', name: 'en0', addr: '203.0.113.42', family: 'v4', index: 1, prefix: 24 },
      { target: 'en0', name: 'en0', addr: '2001:db8::23', family: 'v6', index: 0, prefix: 64 }
    ] },
    { name: 'utun4', up: true, addresses: [
      { target: 'utun4', name: 'utun4', addr: '100.101.102.103', family: 'v4', index: 0, prefix: 32 }
    ] }
  ];

  /* ---- Provider instances (accounts) ---- */
  var providers = [
    { id: 'p-cf', name: 'Homelab Cloudflare', type: 'cloudflare', enabled: true, status: 'ok',
      statusText: 'Validated 12m ago', zones: ['example.com', 'homelab.net'], records: 6,
      config: { api_token: 'cf_••••••••••••7f2a', proxied: true, ttl: 300 },
      lastValidated: at(12), createdAt: at(24 * 60 * 40) },
    { id: 'p-ali', name: 'Aliyun DNS · 主站', type: 'alidns', enabled: true, status: 'ok',
      statusText: 'Validated 1h ago', zones: ['ozhao.cn'], records: 9,
      config: { access_key_id: 'LTAI5t••••••••', access_key_secret: '••••••••••••••••', region: 'cn-hangzhou' },
      lastValidated: at(63), createdAt: at(24 * 60 * 35) },
    { id: 'p-dnspod', name: 'Tencent DNSPod', type: 'dnspod', enabled: true, status: 'warn',
      statusText: 'Token expires in 6 days', zones: ['family.site'], records: 3,
      config: { login_token_id: '447••', login_token: '••••••••••••', record_line: '默认' },
      lastValidated: at(120), createdAt: at(24 * 60 * 12) },
    { id: 'p-gd', name: 'GoDaddy (legacy)', type: 'godaddy', enabled: false, status: 'err',
      statusText: 'Invalid credentials (401)', zones: [], records: 0,
      config: { key: '••••••••', secret: '••••••••••••' },
      lastValidated: at(60 * 20), createdAt: at(24 * 60 * 60) },
    { id: 'p-duck', name: 'DuckDNS', type: 'duckdns', enabled: true, status: 'ok',
      statusText: 'Validated 3h ago', zones: ['ozddns'], records: 1,
      config: { token: '••••••••-••••-••••', subdomain: 'ozddns' },
      lastValidated: at(180), createdAt: at(24 * 60 * 20) },
    { id: 'p-hook', name: 'NAS Callback', type: 'custom', enabled: true, status: 'ok',
      statusText: 'Validated 25m ago', zones: ['—'], records: 0,
      config: { url: 'https://nas.lan/hooks/ddns', method: 'POST', auth: '••••••••' },
      lastValidated: at(25), createdAt: at(24 * 60 * 5) }
  ];

  /* ---- Jobs (sync tasks) ---- */
  var jobs = [
    { id: 'j-home-v4', name: 'Home IPv4', providerId: 'p-cf', ipSourceId: 'src-ipify-v4',
      targets: [{ domain: 'home.example.com', type: 'A', ttl: 300, proxied: true, comment: 'managed by ozddns' }],
      intervalMinutes: 5, runOnChangeOnly: true, jitter: true, enabled: true,
      lastRun: at(2), lastStatus: 'nochange', nextRun: at(-3),
      current: '203.0.113.42', desired: '203.0.113.42', lastChanged: at(60 * 9) },
    { id: 'j-home-v6', name: 'Home IPv6', providerId: 'p-cf', ipSourceId: 'src-ifconfig-v6',
      targets: [{ domain: 'home.example.com', type: 'AAAA', ttl: 300, proxied: false, comment: 'managed by ozddns' }],
      intervalMinutes: 5, runOnChangeOnly: true, jitter: true, enabled: true,
      lastRun: at(2), lastStatus: 'nochange', nextRun: at(-3),
      current: '2001:db8:8a2e:370:7334::1', desired: '2001:db8:8a2e:370:7334::1', lastChanged: at(60 * 30) },
    { id: 'j-nas', name: 'NAS & CDN edge', providerId: 'p-ali', ipSourceId: 'src-en0',
      targets: [
        { domain: 'nas.ozhao.cn', type: 'A', ttl: 600, comment: 'nas' },
        { domain: 'cdn.ozhao.cn', type: 'A', ttl: 600, comment: 'edge cache' }
      ],
      intervalMinutes: 10, runOnChangeOnly: true, jitter: false, enabled: true,
      lastRun: at(7), lastStatus: 'success', nextRun: at(-3),
      current: '198.51.100.7', desired: '198.51.100.7', lastChanged: at(7) },
    { id: 'j-blog', name: 'Blog (DNSPod)', providerId: 'p-dnspod', ipSourceId: 'src-ipify-v4',
      targets: [{ domain: 'blog.family.site', type: 'A', ttl: 120, comment: '' }],
      intervalMinutes: 15, runOnChangeOnly: false, jitter: true, enabled: true,
      lastRun: at(11), lastStatus: 'failed', nextRun: at(-4),
      current: '192.0.2.55', desired: '203.0.113.42',
      error: 'authentication failed: token rejected (401)',
      lastChanged: at(60 * 26) },
    { id: 'j-wild', name: 'Wildcard .ozhao.cn', providerId: 'p-ali', ipSourceId: 'src-ipify-v4',
      targets: [{ domain: '*.ozhao.cn', type: 'A', ttl: 3600, comment: 'catch-all' }],
      intervalMinutes: 30, runOnChangeOnly: true, jitter: true, enabled: false,
      lastRun: at(240), lastStatus: 'nochange', nextRun: null,
      current: '203.0.113.42', desired: '203.0.113.42', lastChanged: at(60 * 40) },
    { id: 'j-iot', name: 'IoT callback', providerId: 'p-hook', ipSourceId: 'src-en0',
      targets: [{ domain: 'iot.homelab.net', type: 'CNAME', ttl: 300, comment: 'webhook target' }],
      intervalMinutes: 60, runOnChangeOnly: true, jitter: false, enabled: true,
      lastRun: at(35), lastStatus: 'success', nextRun: at(-25),
      current: '203.0.113.42', desired: '203.0.113.42', lastChanged: at(35) },
    { id: 'j-tail', name: 'Tailscale node', providerId: 'p-duck', ipSourceId: 'src-cmd-v6',
      targets: [{ domain: 'ozddns.duckdns.org', type: 'AAAA', ttl: 60, comment: '' }],
      intervalMinutes: 2, runOnChangeOnly: true, jitter: false, enabled: true,
      lastRun: at(1), lastStatus: 'skipped', nextRun: at(-1),
      current: '—', desired: '—', note: 'No IPv6 address on this host yet',
      lastChanged: null }
  ];

  /* ---- Execution history (runs) ---- */
  function run(id, jobId, minsAgo, durMs, status, oldIp, newIp, extra) {
    var r = { id: id, jobId: jobId, startedAt: at(minsAgo), durationMs: durMs, status: status,
      oldIp: oldIp || null, newIp: newIp || null, trigger: 'schedule' };
    if (extra) for (var k in extra) r[k] = extra[k];
    return r;
  }
  var runs = [
    run('r-101', 'j-tail', 1, 320, 'skipped', null, null, { message: 'No IPv6 address detected; nothing to update.' }),
    run('r-100', 'j-home-v4', 2, 780, 'nochange', '203.0.113.42', '203.0.113.42', { message: 'Record already matches; skipped write.' }),
    run('r-99', 'j-home-v6', 2, 690, 'nochange', '2001:db8:8a2e:370:7334::1', '2001:db8:8a2e:370:7334::1', { message: 'Record already matches; skipped write.' }),
    run('r-98', 'j-nas', 7, 1420, 'success', '198.51.100.7', '198.51.100.7', { records: 2, message: 'Patched proxied flag on cdn.ozhao.cn.' }),
    run('r-97', 'j-blog', 11, 510, 'failed', null, null, { message: 'authentication failed: token rejected (401)' }),
    run('r-96', 'j-home-v4', 7, 640, 'nochange', '203.0.113.42', '203.0.113.42'),
    run('r-95', 'j-iot', 35, 980, 'success', '203.0.113.42', '203.0.113.42', { message: 'Webhook returned 200 OK.' }),
    run('r-94', 'j-home-v4', 12, 3120, 'success', '203.0.113.9', '203.0.113.42', { message: 'Updated A record for home.example.com.' }),
    run('r-93', 'j-home-v6', 12, 2440, 'success', '2001:db8:8a2e:370:7334::1', '2001:db8:8a2e:370:7334::1'),
    run('r-92', 'j-blog', 26, 480, 'failed', null, null, { message: 'authentication failed: token rejected (401)' }),
    run('r-91', 'j-nas', 17, 1310, 'nochange', '198.51.100.7', '198.51.100.7'),
    run('r-90', 'j-wild', 240, 700, 'nochange', '203.0.113.42', '203.0.113.42'),
    run('r-89', 'j-home-v4', 17, 710, 'nochange', '203.0.113.42', '203.0.113.42'),
    run('r-88', 'j-home-v4', 22, 690, 'nochange', '203.0.113.42', '203.0.113.42'),
    run('r-87', 'j-home-v6', 22, 600, 'nochange', '2001:db8:8a2e:370:7334::1', '2001:db8:8a2e:370:7334::1'),
    run('r-86', 'j-iot', 95, 1120, 'failed', null, null, { message: 'connect: i/o timeout to nas.lan' }),
    run('r-85', 'j-nas', 27, 1490, 'success', '198.51.100.7', '198.51.100.7'),
    run('r-84', 'j-tail', 3, 300, 'skipped', null, null, { message: 'No IPv6 address detected; nothing to update.' }),
    run('r-83', 'j-home-v4', 27, 660, 'nochange', '203.0.113.42', '203.0.113.42'),
    run('r-82', 'j-wild', 300, 640, 'nochange', '203.0.113.42', '203.0.113.42'),
    run('r-81', 'j-home-v4', 32, 3300, 'success', '203.0.113.42', '203.0.113.42', { message: 'No change, forced reconcile.' }),
    run('r-80', 'j-blog', 41, 500, 'failed', null, null, { message: 'authentication failed: token rejected (401)' })
  ];

  /* ---- Activity feed ---- */
  var activity = [
    { at: at(1), kind: 'info', title: 'Scheduler tick', text: 'Evaluated 7 jobs · 2 changed · 4 unchanged · 1 skipped' },
    { at: at(2), kind: 'ok', title: 'Home IPv4 reconciled', text: 'A home.example.com matches 203.0.113.42' },
    { at: at(7), kind: 'ok', title: 'NAS & CDN edge updated', text: 'Patched proxied flag on cdn.ozhao.cn' },
    { at: at(11), kind: 'err', title: 'Blog (DNSPod) failed', text: 'Auth token rejected — see run r-97' },
    { at: at(12), kind: 'ok', title: 'Public IPv4 changed', text: '203.0.113.9 → 203.0.113.42 (source: ipify)' },
    { at: at(35), kind: 'ok', title: 'IoT callback delivered', text: 'POST https://nas.lan/hooks/ddns → 200' },
    { at: at(63), kind: 'info', title: 'Aliyun DNS re-validated', text: 'Zone ozhao.cn reachable, 9 records' }
  ];

  /* ---- Public IP + runtime + settings ---- */
  var ipInfo = {
    v4: '203.0.113.42', v4Source: 'ipify', v4Checked: at(1),
    v6: '2001:db8:8a2e:370:7334::1', v6Source: 'ifconfig.co', v6Checked: at(1)
  };
  var runtime = { scheduler: 'running', uptimeSeconds: 3 * DAY / 1000 + 4 * HOUR / 1000 + 12 * 60,
    version: 'v0.9.0', build: '2026.10.09', dbPath: './data/ozddns.db', dbDriver: 'sqlite',
    listen: '127.0.0.1:9876', listenSource: 'default', goVersion: 'go1.26', goos: 'darwin', goarch: 'arm64' };

  var settings = {
    general: { language: 'en', theme: 'light', defaultInterval: 5, timezone: 'Asia/Shanghai' },
    security: { username: 'admin', passwordSet: true, allowWan: false, tls: false, apiTokens: 1 },
    notify: { webhooks: [
      { id: 'n1', kind: 'slack', name: '#homelab alerts', target: 'https://hooks.slack.com/•••', enabled: true, on: ['failure'] },
      { id: 'n2', kind: 'webhook', name: 'Uptime Kuma', target: 'https://kuma.lan/api/push/•••', enabled: true, on: ['change', 'failure'] }
    ] }
  };

  window.OZ = window.OZ || {};
  window.OZ.data = {
    providerTypes: providerTypes, ipSourceTypes: ipSourceTypes,
    ipSources: ipSources, interfaces: interfaces, providers: providers, jobs: jobs, runs: runs,
    activity: activity, ipInfo: ipInfo, runtime: runtime, settings: settings
  };
})();
