// Preset "Target" values for the IP-source editor.
//
// The Target field is a combo box: it is backed by a <datalist>, so users can
// pick a sensible default (public endpoint, local NIC address, common command)
// or type their own value. Interface entries expose one preset per usable
// address so a NIC with several addresses of a family can be disambiguated
// with the "#n" suffix the resolver understands.

export interface TargetPreset {
  value: string;
  label: string;
}

type InterfaceLike = {
  name?: string;
  addresses?: Array<{ target?: string; addr?: string; family?: string; prefix?: number }>;
};

const HTTP_V4 = [
  "https://api.ipify.org",
  "https://ipv4.icanhazip.com",
  "https://ifconfig.me/ip",
  "https://v4.ident.me",
  "https://checkip.amazonaws.com",
  "https://api.ip.sb/ip"
];

const HTTP_V6 = [
  "https://v6.ifconfig.co",
  "https://ipv6.icanhazip.com",
  "https://v6.ident.me",
  "https://api6.ipify.org",
  "https://api-ipv6.ip.sb/ip"
];

const COMMANDS = [
  "curl -s https://api.ipify.org",
  "curl -s https://v6.ifconfig.co",
  "tailscale ip -6",
  "dig +short myip.opendns.com @resolver1.opendns.com",
  "ip -6 addr show scope global"
];

function httpPresets(family: string): TargetPreset[] {
  return (family === "v6" ? HTTP_V6 : HTTP_V4).map((value) => ({
    value,
    label: value.replace(/^https?:\/\//, "")
  }));
}

function commandPresets(): TargetPreset[] {
  return COMMANDS.map((value) => ({ value, label: value }));
}

export function interfacePresets(interfaces: InterfaceLike[], family: string): TargetPreset[] {
  return (interfaces || []).flatMap((item) =>
    (item.addresses || [])
      .filter((address) => address.family === family && address.target)
      .map((address) => ({
        value: String(address.target),
        label: `${item.name} · ${address.addr}${address.prefix ? `/${address.prefix}` : ""}`
      })));
}

// targetPresets returns the dropdown suggestions for a source kind + family.
export function targetPresets(type: string, family: string, interfaces: InterfaceLike[]): TargetPreset[] {
  switch (type) {
    case "interface":
      return interfacePresets(interfaces, family);
    case "http":
      return httpPresets(family);
    case "command":
      return commandPresets();
    default:
      return [];
  }
}

// defaultTarget seeds the field when the kind (or, for interface/static
// sources, the family) changes. It never overrides a value the user typed.
export function defaultTarget(type: string, family: string, interfaces: InterfaceLike[]): string {
  switch (type) {
    case "http":
      return family === "v6" ? HTTP_V6[0] : HTTP_V4[0];
    case "interface":
      return interfacePresets(interfaces, family)[0]?.value || "";
    case "command":
      return COMMANDS[0];
    case "static":
      return family === "v6" ? "2001:db8::1" : "203.0.113.42";
    default:
      return "";
  }
}

export function targetPlaceholder(type: string, family: string): string {
  switch (type) {
    case "interface":
      return family === "v6" ? "en0 · 2001:db8::23/64" : "en0 · 192.168.1.23/24";
    case "command":
      return "tailscale ip -6";
    case "static":
      return family === "v6" ? "2001:db8::1" : "203.0.113.42";
    default:
      return "https://api.ipify.org";
  }
}
