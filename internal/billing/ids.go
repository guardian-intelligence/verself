package billing

import (
	"encoding/binary"
	"time"

	"github.com/tigerbeetle/tigerbeetle-go/pkg/types"
)

type (
	OrgID          uint64
	JobID          int64
	SubscriptionID int64
	TaskID         int64
	GrantID        int64
)

type (
	AccountID  struct{ raw types.Uint128 }
	TransferID struct{ raw types.Uint128 }
)

func GrantAccountID(grant GrantID) AccountID {
	var id [16]byte
	binary.LittleEndian.PutUint16(id[0:2], uint16(AcctGrant))
	binary.LittleEndian.PutUint64(id[8:16], uint64(grant))
	return AccountID{raw: types.BytesToUint128(id)}
}

func OperatorAccountID(t OperatorAcctType) AccountID {
	var id [16]byte
	binary.LittleEndian.PutUint16(id[0:2], uint16(t))
	return AccountID{raw: types.BytesToUint128(id)}
}

func VMTransferID(job JobID, seq uint32, grantIdx uint8, kind XferKind) TransferID {
	var id [16]byte
	binary.LittleEndian.PutUint32(id[0:4], seq)
	id[4] = grantIdx
	id[5] = uint8(kind)
	binary.LittleEndian.PutUint64(id[8:16], uint64(job))
	return TransferID{raw: types.BytesToUint128(id)}
}

func SubscriptionPeriodID(sub SubscriptionID, periodStart time.Time, kind XferKind) TransferID {
	t := periodStart.UTC()
	var id [16]byte
	binary.LittleEndian.PutUint32(id[0:4], uint32(t.Year())*12+uint32(t.Month()))
	id[5] = uint8(kind)
	binary.LittleEndian.PutUint64(id[8:16], uint64(sub))
	return TransferID{raw: types.BytesToUint128(id)}
}

func StripeDepositID(task TaskID, kind XferKind) TransferID {
	var id [16]byte
	id[5] = uint8(kind)
	binary.LittleEndian.PutUint64(id[8:16], uint64(task))
	return TransferID{raw: types.BytesToUint128(id)}
}

func CreditExpiryID(grant GrantID) TransferID {
	var id [16]byte
	id[5] = uint8(KindCreditExpiry)
	binary.LittleEndian.PutUint64(id[8:16], uint64(grant))
	return TransferID{raw: types.BytesToUint128(id)}
}

func (a AccountID) ParseGrant() (grant GrantID, acctType uint16) {
	b := a.raw.Bytes()
	acctType = binary.LittleEndian.Uint16(b[0:2])
	grant = GrantID(binary.LittleEndian.Uint64(b[8:16]))
	return
}

func (t TransferID) Parse() (sourceID uint64, seq uint32, grantIdx uint8, kind uint8) {
	b := t.raw.Bytes()
	seq = binary.LittleEndian.Uint32(b[0:4])
	grantIdx = b[4]
	kind = b[5]
	sourceID = binary.LittleEndian.Uint64(b[8:16])
	return
}
