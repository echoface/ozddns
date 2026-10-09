package app

import (
	"strings"

	"github.com/echoface/ozddns/internal/provider"
)

// MaskConfig returns a copy of a provider config with secret fields masked so it
// is safe to return over the API or print in the CLI.
func MaskConfig(reg *provider.Registry, typ string, cfg map[string]any) map[string]any {
	out := make(map[string]any, len(cfg))
	secrets := map[string]bool{}
	if f, ok := reg.Get(typ); ok {
		for _, fld := range f.Meta().Fields {
			if fld.Secret || fld.Type == provider.FieldSecret {
				secrets[fld.Key] = true
			}
		}
	}
	for k, v := range cfg {
		if secrets[k] {
			if s, ok := v.(string); ok {
				out[k] = maskSecret(s)
				continue
			}
		}
		out[k] = v
	}
	return out
}

func maskSecret(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if len(s) <= 4 {
		return "••••"
	}
	return "••••" + s[len(s)-4:]
}

// IsMaskedSecret reports whether v is a mask produced by MaskConfig (rather than
// a real credential).
func IsMaskedSecret(v any) bool {
	s, ok := v.(string)
	return ok && strings.Contains(s, "••")
}

// secretKeys returns the config keys of a provider type that hold credentials.
func secretKeys(reg *provider.Registry, typ string) map[string]bool {
	out := map[string]bool{}
	f, ok := reg.Get(typ)
	if !ok {
		return out
	}
	for _, fld := range f.Meta().Fields {
		if fld.Secret || fld.Type == provider.FieldSecret {
			out[fld.Key] = true
		}
	}
	return out
}

// MergeSecrets returns incoming with every secret field that was left
// untouched replaced by the value already stored. A secret is "untouched" when
// it is absent, blank, or still carries its mask — the panel deliberately
// renders stored secrets as an empty input (with a "saved" chip) so the real
// credential is never sent to the browser. Without this merge, saving the form
// would either persist the mask itself or blank the credential.
func MergeSecrets(reg *provider.Registry, typ string, incoming, existing map[string]any) map[string]any {
	out := make(map[string]any, len(incoming))
	for k, v := range incoming {
		out[k] = v
	}
	for key := range secretKeys(reg, typ) {
		if v, ok := out[key]; ok && !IsMaskedSecret(v) && !isBlankSecret(v) {
			continue // a real replacement credential was supplied
		}
		if prev, ok := existing[key]; ok {
			out[key] = prev
		} else {
			delete(out, key) // nothing to keep: drop rather than store a mask/blank
		}
	}
	return out
}

// isBlankSecret reports whether v is an empty credential (as opposed to a real
// value or a mask), so that "leave the field blank" keeps the stored secret.
func isBlankSecret(v any) bool {
	s, ok := v.(string)
	return ok && strings.TrimSpace(s) == ""
}

// MaskedSecretFields lists, for a config, the keys still carrying their mask.
func MaskedSecretFields(reg *provider.Registry, typ string, cfg map[string]any) []string {
	var out []string
	for key := range secretKeys(reg, typ) {
		if IsMaskedSecret(cfg[key]) {
			out = append(out, key)
		}
	}
	return out
}
