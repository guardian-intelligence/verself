package billing

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"hash/fnv"

	"github.com/oklog/ulid/v2"
	"github.com/tigerbeetle/tigerbeetle-go/pkg/types"
)

type GrantID [16]byte

type (
	AccountID  struct{ raw types.Uint128 }
	TransferID struct{ raw types.Uint128 }
)

func NewGrantID() GrantID {
	return GrantID(ulid.Make())
}

func ParseGrantID(value string) (GrantID, error) {
	parsed, err := ulid.ParseStrict(value)
	if err != nil {
		return GrantID{}, fmt.Errorf("parse grant id %q: %w", value, err)
	}
	return GrantID(parsed), nil
}

func (g GrantID) String() string {
	return ulid.ULID(g).String()
}

func (g GrantID) MarshalText() ([]byte, error) {
	return []byte(g.String()), nil
}

func (g *GrantID) UnmarshalText(text []byte) error {
	parsed, err := ParseGrantID(string(text))
	if err != nil {
		return err
	}
	*g = parsed
	return nil
}

func grantHalfSwap(grant GrantID) types.Uint128 {
	var buf [16]byte
	binary.LittleEndian.PutUint64(buf[0:8], binary.BigEndian.Uint64(grant[8:16]))
	binary.LittleEndian.PutUint64(buf[8:16], binary.BigEndian.Uint64(grant[0:8]))
	return types.BytesToUint128(buf)
}

func GrantAccountID(grant GrantID) AccountID {
	return AccountID{raw: grantHalfSwap(grant)}
}

func OperatorAccountID(t OperatorAcctType) AccountID {
	var id [16]byte
	binary.LittleEndian.PutUint16(id[0:2], uint16(t))
	return AccountID{raw: types.BytesToUint128(id)}
}

func WindowTransferID(windowID string, legIndex uint8, kind XferKind) TransferID {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s:%d:%d", windowID, legIndex, kind)))
	var raw [16]byte
	copy(raw[:], sum[:16])
	raw[5] = uint8(kind)
	raw[4] = legIndex
	return TransferID{raw: types.BytesToUint128(raw)}
}

func WindowLookupSeed(windowID string) types.Uint128 {
	sum := sha256.Sum256([]byte(windowID))
	var raw [16]byte
	copy(raw[:], sum[:16])
	return types.BytesToUint128(raw)
}

func ProductPeriodAccountID(orgID OrgID, productID string, periodKey string) AccountID {
	h := fnv.New64a()
	_, _ = h.Write([]byte(productID))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(periodKey))
	hash := h.Sum64()

	var id [16]byte
	binary.LittleEndian.PutUint64(id[0:8], hash)
	binary.LittleEndian.PutUint64(id[8:16], uint64(orgID))
	return AccountID{raw: types.BytesToUint128(id)}
}

func (a AccountID) Raw() types.Uint128 { return a.raw }

func (t TransferID) Raw() types.Uint128 { return t.raw }

func (t TransferID) String() string {
	b := t.raw.Bytes()
	return hex.EncodeToString(b[:])
}

func (t TransferID) MarshalText() ([]byte, error) {
	return []byte(t.String()), nil
}

func (t *TransferID) UnmarshalText(text []byte) error {
	parsed, err := ParseTransferID(string(text))
	if err != nil {
		return err
	}
	*t = parsed
	return nil
}

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
