package billing

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"hash/fnv"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/tigerbeetle/tigerbeetle-go/pkg/types"
)

type (
	OrgID          uint64
	JobID          int64
	SubscriptionID int64
	TaskID         int64

	// GrantID is an application-generated ULID (128-bit, time-ordered).
	// Decoupled from PostgreSQL sequence state — the grant's TigerBeetle
	// identity survives database recreation.
	GrantID [16]byte
)

type (
	AccountID  struct{ raw types.Uint128 }
	TransferID struct{ raw types.Uint128 }
)

// NewGrantID generates a fresh ULID for a credit grant.
// Uses oklog/ulid's default monotonic entropy source: IDs generated within
// the same millisecond are strictly increasing, which is what TigerBeetle's
// LSM tree benefits from.
func NewGrantID() GrantID {
	return GrantID(ulid.Make())
}

// String returns the grant ULID in its canonical text form.
func (g GrantID) String() string {
	return ulid.ULID(g).String()
}

// ParseGrantID parses a canonical ULID text value into a GrantID.
func ParseGrantID(value string) (GrantID, error) {
	parsed, err := ulid.ParseStrict(value)
	if err != nil {
		return GrantID{}, fmt.Errorf("parse grant id %q: %w", value, err)
	}
	return GrantID(parsed), nil
}

// grantHalfSwap builds a 16-byte Uint128 from a ULID by swapping its two
// big-endian 8-byte halves into TigerBeetle's little-endian layout.
//
// Uint128 bytes [0:8] (low u64, LE) ← ULID bytes [8:16] (random tail, BE→LE)
// Uint128 bytes [8:16] (high u64, LE) ← ULID bytes [0:8] (timestamp+random, BE→LE)
//
// This places the ULID's 48-bit timestamp in the high u64 where TigerBeetle's
// LSM tree benefits from monotonic ordering. The mapping is bijective.
func grantHalfSwap(grant GrantID) types.Uint128 {
	var buf [16]byte
	// Low u64: ULID random tail (bytes 8-15), big-endian → little-endian
	binary.LittleEndian.PutUint64(buf[0:8], binary.BigEndian.Uint64(grant[8:16]))
	// High u64: ULID timestamp + random head (bytes 0-7), big-endian → little-endian
	binary.LittleEndian.PutUint64(buf[8:16], binary.BigEndian.Uint64(grant[0:8]))
	return types.BytesToUint128(buf)
}

// GrantAccountID maps a ULID to a TigerBeetle account ID via the half-swap.
func GrantAccountID(grant GrantID) AccountID {
	return AccountID{raw: grantHalfSwap(grant)}
}

// CreditExpiryID uses the same half-swap into the TransferID namespace.
// Account IDs and transfer IDs are separate TigerBeetle namespaces, so the
// same numeric value in both is safe. There is exactly one expiry transfer
// per grant.
func CreditExpiryID(grant GrantID) TransferID {
	return TransferID{raw: grantHalfSwap(grant)}
}

// CreditExpiryPostID returns a deterministic post-pending transfer ID for
// grant expiry. Derived from the grant ULID with KindExpiryConfirm packed
// into byte 5 of the low u64. This is collision-safe: the pending expiry ID
// (from grantHalfSwap) has the ULID's random tail in bytes 0-7, while this
// ID overwrites byte 5 with a known constant, producing a different value.
func CreditExpiryPostID(grant GrantID) TransferID {
	b := grantHalfSwap(grant).Bytes()
	b[5] ^= 0xFF // flip all bits to guarantee difference from CreditExpiryID
	return TransferID{raw: types.BytesToUint128(b)}
}

// Raw returns the underlying TigerBeetle Uint128 for direct TB API calls.
func (a AccountID) Raw() types.Uint128 { return a.raw }

// Raw returns the underlying TigerBeetle Uint128 for direct TB API calls.
func (t TransferID) Raw() types.Uint128 { return t.raw }

// String returns a stable hex encoding of the raw transfer ID bytes.
func (t TransferID) String() string {
	b := t.raw.Bytes()
	return hex.EncodeToString(b[:])
}

// ParseTransferID parses a hex-encoded transfer ID from its raw 16-byte form.
func ParseTransferID(value string) (TransferID, error) {
	raw, err := hex.DecodeString(value)
	if err != nil {
		return TransferID{}, fmt.Errorf("parse transfer id %q: %w", value, err)
	}
	if len(raw) != 16 {
		return TransferID{}, fmt.Errorf("parse transfer id %q: expected 16 bytes, got %d", value, len(raw))
	}
	var buf [16]byte
	copy(buf[:], raw)
	return TransferID{raw: types.BytesToUint128(buf)}, nil
}

// GrantULID recovers the original ULID from a grant account ID (reverses
// the half-swap).
func (a AccountID) GrantULID() GrantID {
	var g GrantID
	b := a.raw.Bytes()
	// Reverse: high u64 (LE) → ULID bytes 0:8 (BE)
	binary.BigEndian.PutUint64(g[0:8], binary.LittleEndian.Uint64(b[8:16]))
	// Reverse: low u64 (LE) → ULID bytes 8:16 (BE)
	binary.BigEndian.PutUint64(g[8:16], binary.LittleEndian.Uint64(b[0:8]))
	return g
}

// OperatorAccountID builds a small sentinel ID with the type in the low 16
// bits and zeros in the high u64. These never overlap with grant account IDs
// because any ULID generated after Unix epoch has a nonzero high u64 after
// the half-swap.
func OperatorAccountID(t OperatorAcctType) AccountID {
	var id [16]byte
	binary.LittleEndian.PutUint16(id[0:2], uint16(t))
	return AccountID{raw: types.BytesToUint128(id)}
}

// VMTransferID packs (job_id, window_seq, grant_idx, kind) into a transfer ID.
// source_id (job_id) goes in the high u64 for LSM ordering.
func VMTransferID(job JobID, seq uint32, grantIdx uint8, kind XferKind) TransferID {
	var id [16]byte
	binary.LittleEndian.PutUint32(id[0:4], seq)
	id[4] = grantIdx
	id[5] = uint8(kind)
	binary.LittleEndian.PutUint64(id[8:16], uint64(job))
	return TransferID{raw: types.BytesToUint128(id)}
}

// SubscriptionPeriodID packs (subscription_id, year_month, kind) into a
// transfer ID. Deterministic for a given (subscription, period, kind) triple,
// which is what makes deposit idempotency work across cron and webhook paths.
func SubscriptionPeriodID(sub SubscriptionID, periodStart time.Time, kind XferKind) TransferID {
	t := periodStart.UTC()
	var id [16]byte
	binary.LittleEndian.PutUint32(id[0:4], uint32(t.Year())*12+uint32(t.Month()))
	id[5] = uint8(kind)
	binary.LittleEndian.PutUint64(id[8:16], uint64(sub))
	return TransferID{raw: types.BytesToUint128(id)}
}

// StripeDepositID packs (task_id, kind) into a transfer ID. One transfer per
// task — the task's idempotency_key (Stripe event ID) is the first layer,
// this deterministic transfer ID is the second.
func StripeDepositID(task TaskID, kind XferKind) TransferID {
	var id [16]byte
	id[5] = uint8(kind)
	binary.LittleEndian.PutUint64(id[8:16], uint64(task))
	return TransferID{raw: types.BytesToUint128(id)}
}

// DisputeDebitID packs (task_id, grant_idx, KindDisputeDebit) into a transfer ID.
// One transfer per (task, grant) pair — supports debiting up to 256 grants per dispute.
func DisputeDebitID(task TaskID, grantIdx uint8) TransferID {
	var id [16]byte
	id[4] = grantIdx
	id[5] = uint8(KindDisputeDebit)
	binary.LittleEndian.PutUint64(id[8:16], uint64(task))
	return TransferID{raw: types.BytesToUint128(id)}
}

// QuotaAccountID builds a deterministic account ID for a quota limit.
// High u64: org_id (LSM locality — same org's accounts are adjacent).
// Low u64: FNV-1a(product_id + "\x00" + dimension + "\x00" + window) with
// the quota code discriminator in bytes 6-7 to avoid collisions with grant
// or operator accounts.
func QuotaAccountID(orgID OrgID, productID, dimension, window string) AccountID {
	h := fnv.New64a()
	h.Write([]byte(productID))
	h.Write([]byte{0})
	h.Write([]byte(dimension))
	h.Write([]byte{0})
	h.Write([]byte(window))
	hash := h.Sum64()

	var id [16]byte
	binary.LittleEndian.PutUint64(id[0:8], hash)
	// Stamp quota code into bytes 6-7 to disambiguate from grant accounts.
	binary.LittleEndian.PutUint16(id[6:8], AcctQuotaCode)
	binary.LittleEndian.PutUint64(id[8:16], uint64(orgID))
	return AccountID{raw: types.BytesToUint128(id)}
}

// OverageCapAccountID builds a deterministic account ID for an overage cap.
// Same layout as QuotaAccountID but with OverageCapCode discriminator.
func OverageCapAccountID(orgID OrgID, productID string) AccountID {
	h := fnv.New64a()
	h.Write([]byte(productID))
	hash := h.Sum64()

	var id [16]byte
	binary.LittleEndian.PutUint64(id[0:8], hash)
	binary.LittleEndian.PutUint16(id[6:8], AcctOverageCapCode)
	binary.LittleEndian.PutUint64(id[8:16], uint64(orgID))
	return AccountID{raw: types.BytesToUint128(id)}
}

// QuotaTransferID builds a unique transfer ID for a quota check pending transfer.
// Uses nanosecond timestamp + dimension hash for uniqueness (quota transfers are
// never posted or voided by the application — they expire naturally).
func QuotaTransferID(orgID OrgID, dimension string, nanoTS int64) TransferID {
	h := fnv.New32a()
	h.Write([]byte(dimension))

	var id [16]byte
	binary.LittleEndian.PutUint32(id[0:4], h.Sum32())
	id[5] = uint8(KindQuotaCheck)
	binary.LittleEndian.PutUint64(id[8:16], uint64(nanoTS))
	return TransferID{raw: types.BytesToUint128(id)}
}

// OverageCapTransferID builds a transfer ID for overage cap operations.
func OverageCapTransferID(jobID JobID, windowSeq uint32, kind XferKind) TransferID {
	var id [16]byte
	binary.LittleEndian.PutUint32(id[0:4], windowSeq)
	id[4] = 0xFF // distinguishes from grant leg transfers (grantIdx < 256)
	id[5] = uint8(kind)
	binary.LittleEndian.PutUint64(id[8:16], uint64(jobID))
	return TransferID{raw: types.BytesToUint128(id)}
}

// Parse extracts the packed fields from a non-ULID transfer ID.
// Does not apply to credit expiry transfers (those use the ULID half-swap).
func (t TransferID) Parse() (sourceID uint64, seq uint32, grantIdx uint8, kind uint8) {
	b := t.raw.Bytes()
	seq = binary.LittleEndian.Uint32(b[0:4])
	grantIdx = b[4]
	kind = b[5]
	sourceID = binary.LittleEndian.Uint64(b[8:16])
	return
}
