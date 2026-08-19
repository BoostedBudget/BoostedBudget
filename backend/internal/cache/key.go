package cache

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"sort"
	"strconv"
	"time"
)

// keyPrefix is the fixed root for every key this package renders.
const keyPrefix = "bb:c"

// ScopeTag renders the Redis Cluster hash-tag portion of a key for the given scope kind and id.
// A generation key and its data keys share this tag so Redis Cluster routes them to the same
// slot. Clerk IDs are hashed (semi-public identifiers, per the brief); internal UUIDs are kept raw
// for operator debuggability.
func ScopeTag(kind ScopeKind, id string) string {
	switch kind {
	case ScopeClerkID:
		sum := sha256.Sum256([]byte(id))
		return "k:" + hex.EncodeToString(sum[:])[:16]
	case ScopeHousehold:
		return "h:" + id
	default: // ScopeUser and anything unrecognised fall back to the user prefix
		return "u:" + id
	}
}

// GenerationKey returns the Redis key holding scopeTag's generation token:
// bb:c:{<scopeTag>}:gen
func GenerationKey(scopeTag string) string {
	return fmt.Sprintf("%s:{%s}:gen", keyPrefix, scopeTag)
}

// ControlKey returns the Redis key holding ns's kill-switch value: bb:c:ctl:v1:<ns>
func ControlKey(ns Namespace) string {
	return fmt.Sprintf("%s:ctl:v1:%s", keyPrefix, ns)
}

// Key identifies one cached value, before rendering into a generation-qualified Redis key string.
// Method MUST be a `const` string literal at the call site — never derived via reflection — so a
// method rename is a compile error rather than a silent key-space change.
type Key struct {
	Namespace Namespace
	ScopeKind ScopeKind
	ScopeID   string
	Method    string
	ArgHash   string // hex, from (*ArgEncoder).Hash()
}

// ScopeTag renders this key's Redis Cluster hash tag.
func (k Key) ScopeTag() string {
	return ScopeTag(k.ScopeKind, k.ScopeID)
}

// GenerationKey returns the Redis key holding this key's generation token.
func (k Key) GenerationKey() string {
	return GenerationKey(k.ScopeTag())
}

// DataKey renders the full Redis key for this Key at the given schema version and generation
// token: bb:c:{<scopeTag>}:<ns>:v<schemaVer>:g<gen>:<method>:<argHash>
func (k Key) DataKey(schemaVersion int, generation string) string {
	return fmt.Sprintf("%s:{%s}:%s:v%d:g%s:%s:%s",
		keyPrefix, k.ScopeTag(), k.Namespace, schemaVersion, generation, k.Method, k.ArgHash)
}

// ArgEncoder builds a type-tagged, length-prefixed byte encoding of a method's arguments, then
// hashes it. Collision safety is structural: every method writes its own type tag and a length (or
// count) prefix before its payload, so ("ab","c") cannot collide with ("a","bc") and a 2-element
// list cannot collide with a 1-element list whose single string happens to be their concatenation.
//
// No reflection. Every call site hand-writes its sequence of encoder calls, so it is auditable —
// see the package doc comment on why that matters here.
type ArgEncoder struct {
	buf bytes.Buffer
}

// NewArgEncoder returns an empty encoder.
func NewArgEncoder() *ArgEncoder {
	return &ArgEncoder{}
}

func (e *ArgEncoder) writeUvarint(v uint64) {
	var tmp [binary.MaxVarintLen64]byte
	n := binary.PutUvarint(tmp[:], v)
	e.buf.Write(tmp[:n])
}

// String encodes s as 's' + uvarint(len(s)) + bytes.
func (e *ArgEncoder) String(s string) {
	e.buf.WriteByte('s')
	e.writeUvarint(uint64(len(s)))
	e.buf.WriteString(s)
}

// Int encodes i as 'i' + uvarint(zigzag(i)).
func (e *ArgEncoder) Int(i int) {
	e.buf.WriteByte('i')
	e.writeUvarint(zigzagEncode(int64(i)))
}

func zigzagEncode(v int64) uint64 {
	return uint64((v << 1) ^ (v >> 63))
}

// Bool encodes b as the literal two-byte tag "b0" or "b1".
func (e *ArgEncoder) Bool(b bool) {
	if b {
		e.buf.WriteString("b1")
	} else {
		e.buf.WriteString("b0")
	}
}

// Float encodes f as 'f' + strconv.FormatFloat(f, 'x', -1, 64) — the hex float format, chosen so
// that f and math.Nextafter(f, ...) can never collide the way they could under '%v' (which renders
// both 2.0 and its next representable neighbour as "2").
func (e *ArgEncoder) Float(f float64) {
	e.buf.WriteByte('f')
	e.buf.WriteString(strconv.FormatFloat(f, 'x', -1, 64))
}

// Time encodes t as 'Z' if t.IsZero(), else 'T' + t.UTC().Format(time.RFC3339Nano). Two instants
// in different zones that denote the same moment hash identically (both normalize to UTC); the Go
// zero value is distinct from the Unix epoch (their RFC3339Nano-in-UTC strings differ).
func (e *ArgEncoder) Time(t time.Time) {
	if t.IsZero() {
		e.buf.WriteByte('Z')
		return
	}
	e.buf.WriteByte('T')
	e.buf.WriteString(t.UTC().Format(time.RFC3339Nano))
}

// TimePtr encodes p as 'N' if nil, else delegates to Time.
func (e *ArgEncoder) TimePtr(p *time.Time) {
	if p == nil {
		e.buf.WriteByte('N')
		return
	}
	e.Time(*p)
}

// StringSetSorted encodes v as 'N' if nil, else 'L' + uvarint(count) + each deduped, sorted
// element (each itself length-prefixed via String). Sorting/deduping makes the encoding
// order-independent — the intended semantics for a filter that means "any of these", not "in this
// order". A nil slice and an empty (non-nil) slice hash differently ('N' vs 'L'+uvarint(0)).
func (e *ArgEncoder) StringSetSorted(v []string) {
	if v == nil {
		e.buf.WriteByte('N')
		return
	}
	seen := make(map[string]struct{}, len(v))
	out := make([]string, 0, len(v))
	for _, s := range v {
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	sort.Strings(out)
	e.buf.WriteByte('L')
	e.writeUvarint(uint64(len(out)))
	for _, s := range out {
		e.String(s)
	}
}

// StringListOrdered encodes v as 'N' if nil, else 'O' + uvarint(len(v)) + each element in order
// (each itself length-prefixed via String). Unlike StringSetSorted, order and duplicates matter.
func (e *ArgEncoder) StringListOrdered(v []string) {
	if v == nil {
		e.buf.WriteByte('N')
		return
	}
	e.buf.WriteByte('O')
	e.writeUvarint(uint64(len(v)))
	for _, s := range v {
		e.String(s)
	}
}

// Hash returns the first 128 bits of SHA-256 over everything encoded so far, hex-encoded.
func (e *ArgEncoder) Hash() string {
	sum := sha256.Sum256(e.buf.Bytes())
	return hex.EncodeToString(sum[:16])
}
