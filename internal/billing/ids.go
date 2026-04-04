package billing

import (
	"encoding/binary"
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
