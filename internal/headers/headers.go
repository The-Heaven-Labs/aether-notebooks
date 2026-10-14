// Package headers implements custom HTTP header maps whose sensitive values
// are encrypted at rest, redacted in API responses, and decrypted only where a
// request is actually made. It is shared by the REST handlers (internal/api)
// and the runtime (internal/agent) so write and read semantics cannot drift.
package headers

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/the-heaven-labs/aether/internal/crypto"
)

// RedactedSentinel is the placeholder returned to clients in place of a stored
// sensitive value. Sending it back on update keeps the stored value.
const RedactedSentinel = "__AETHER_REDACTED__"

const (
	maxHeaders     = 50
	maxNameLength  = 128
	maxValueLength = 8192
)

// tokenRe matches RFC 7230 header field names.
var tokenRe = regexp.MustCompile("^[!#$%&'*+\\-.^_`|~0-9A-Za-z]+$")

// reserved headers are managed by the HTTP transport or by the JSON wire
// format; overriding them can only break the request.
var reserved = map[string]bool{
	"host":           true,
	"content-length": true,
	"content-type":   true,
}

// Apply sets the given header values on req, skipping reserved names. Write
// paths reject reserved headers (Host, Content-Length, Content-Type), but a
// skip here keeps a direct-DB edit from silently breaking the wire format.
func Apply(req *http.Request, values map[string]string) {
	for name, value := range values {
		if reserved[strings.ToLower(name)] {
			continue
		}
		req.Header.Set(name, value)
	}
}

// Validate checks an incoming header map and its sensitive-name list. It is
// called on every write path before values are encrypted.
func Validate(headersIn map[string]any, secrets []string) error {
	if len(headersIn) > maxHeaders {
		return fmt.Errorf("too many headers: %d (max %d)", len(headersIn), maxHeaders)
	}
	seen := make(map[string]string, len(headersIn))
	for name, raw := range headersIn {
		if name == "" || len(name) > maxNameLength {
			return fmt.Errorf("invalid header name %q", name)
		}
		if !tokenRe.MatchString(name) {
			return fmt.Errorf("invalid header name %q (must be an RFC 7230 token)", name)
		}
		if reserved[strings.ToLower(name)] {
			return fmt.Errorf("header %q is reserved and cannot be set", name)
		}
		lower := strings.ToLower(name)
		if prev, dup := seen[lower]; dup {
			return fmt.Errorf("duplicate header %q (also configured as %q)", name, prev)
		}
		seen[lower] = name
		value, ok := raw.(string)
		if !ok {
			return fmt.Errorf("header %q value must be a string", name)
		}
		if len(value) > maxValueLength {
			return fmt.Errorf("header %q value exceeds %d bytes", name, maxValueLength)
		}
	}
	for _, secret := range secrets {
		if !containsName(headersIn, secret) {
			return fmt.Errorf("sensitive header %q is not configured", secret)
		}
	}
	return nil
}

// EncryptSecrets returns a copy of the header map where every value named in
// secrets (case-insensitive) is stored as tagged ciphertext. Values already
// carrying the encryption tag are left as-is, so re-saving never
// double-encrypts and legacy plaintext rows upgrade on their next save.
func EncryptSecrets(headersIn map[string]any, secrets []string, key []byte) (map[string]any, error) {
	out := make(map[string]any, len(headersIn))
	if len(headersIn) == 0 {
		return out, nil
	}
	secretSet := toSet(secrets)
	for name, raw := range headersIn {
		value := fmt.Sprintf("%v", raw)
		if !secretSet[strings.ToLower(name)] {
			out[name] = value
			continue
		}
		if crypto.IsEncryptedString(value) {
			out[name] = value
			continue
		}
		encrypted, err := crypto.EncryptString(value, key)
		if err != nil {
			return nil, fmt.Errorf("encrypt header %q: %w", name, err)
		}
		out[name] = encrypted
	}
	return out, nil
}

// DecryptSecrets resolves a runtime header table: tagged ciphertext is
// decrypted with key, everything else passes through as plaintext.
func DecryptSecrets(headersIn map[string]any, secrets []string, key []byte) (map[string]string, error) {
	out := make(map[string]string, len(headersIn))
	secretSet := toSet(secrets)
	for name, raw := range headersIn {
		value := fmt.Sprintf("%v", raw)
		if !secretSet[strings.ToLower(name)] || !crypto.IsEncryptedString(value) {
			out[name] = value
			continue
		}
		decrypted, err := crypto.DecryptString(value, key)
		if err != nil {
			return nil, fmt.Errorf("decrypt header %q: %w", name, err)
		}
		out[name] = decrypted
	}
	return out, nil
}

// RedactSecrets replaces sensitive values with RedactedSentinel in place, for
// API responses. Ciphertext must never reach a client.
func RedactSecrets(headersIn map[string]any, secrets []string) {
	secretSet := toSet(secrets)
	for name := range headersIn {
		if secretSet[strings.ToLower(name)] {
			headersIn[name] = RedactedSentinel
		}
	}
}

// RestoreSecrets applies update semantics in place: a sentinel value is
// restored from oldHeaders when the name is still sensitive and the old map
// holds a value for it. Otherwise the entry is deleted — the name is no longer
// sensitive, or there is nothing to restore — so a sentinel can never be
// persisted or sent literally. Fresh (non-sentinel) values are untouched.
func RestoreSecrets(oldHeaders, newHeaders map[string]any, secrets []string) {
	secretSet := toSet(secrets)
	for name, raw := range newHeaders {
		value, ok := raw.(string)
		if !ok || value != RedactedSentinel {
			continue
		}
		if secretSet[strings.ToLower(name)] {
			if oldValue, exists := findName(oldHeaders, name); exists {
				newHeaders[name] = oldValue
				continue
			}
		}
		// No stored value to restore (or the name is no longer sensitive):
		// drop the sentinel rather than persisting or sending it literally.
		delete(newHeaders, name)
	}
}

// DropOrphanSentinels removes sentinel-valued entries for names in secrets.
// A create has nothing to restore, so a sentinel there would otherwise be
// encrypted and sent literally; this mirrors RestoreSecrets' orphan deletion.
// Returns the pruned secret-name list (never nil).
func DropOrphanSentinels(headersIn map[string]any, secrets []string) []string {
	secretSet := toSet(secrets)
	for name, raw := range headersIn {
		value, ok := raw.(string)
		if ok && value == RedactedSentinel && secretSet[strings.ToLower(name)] {
			delete(headersIn, name)
		}
	}
	return PresentSecrets(headersIn, secrets)
}

// PresentSecrets returns the subset of secrets whose names exist in headersIn
// (case-insensitive). Use after RestoreSecrets so a deleted sentinel cannot
// leave a dangling secret name behind.
func PresentSecrets(headersIn map[string]any, secrets []string) []string {
	out := make([]string, 0, len(secrets))
	for _, name := range secrets {
		if containsName(headersIn, name) {
			out = append(out, name)
		}
	}
	return out
}

// EncryptUpdate prepares an incoming header map for storage: sentinel values
// are restored from the stored map (when still sensitive), secret names whose
// entries were dropped are pruned, and sensitive values are encrypted.
// Returns the map to store and the pruned secret-name list.
func EncryptUpdate(incoming map[string]any, secrets []string, stored map[string]any, key []byte) (map[string]any, []string, error) {
	RestoreSecrets(stored, incoming, secrets)
	kept := PresentSecrets(incoming, secrets)
	encrypted, err := EncryptSecrets(incoming, kept, key)
	return encrypted, kept, err
}

// NamesFromJSON normalizes a JSON-decoded secrets list ([]any of strings,
// []string, or nil) into a string slice. Non-string entries are ignored.
func NamesFromJSON(v any) []string {
	switch list := v.(type) {
	case []string:
		return list
	case []any:
		out := make([]string, 0, len(list))
		for _, item := range list {
			if name, ok := item.(string); ok {
				out = append(out, name)
			}
		}
		return out
	default:
		return nil
	}
}

func toSet(names []string) map[string]bool {
	set := make(map[string]bool, len(names))
	for _, name := range names {
		set[strings.ToLower(name)] = true
	}
	return set
}

func containsName(headersIn map[string]any, name string) bool {
	_, ok := findName(headersIn, name)
	return ok
}

func findName(headersIn map[string]any, name string) (any, bool) {
	lower := strings.ToLower(name)
	for k, v := range headersIn {
		if strings.ToLower(k) == lower {
			return v, true
		}
	}
	return nil, false
}
