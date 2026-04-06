package fastsandbox

import (
	"encoding/binary"
	"fmt"
)

// Wire protocol constants matching host_ops_proto.zig.
const (
	opsRequestMagic  = 0x48534d10
	opsResponseMagic = 0x48534d11
	opsProtoVersion  = 1
	opsHeaderSize    = 18
	opsMaxMessage    = 4096
	opsMaxPayload    = opsMaxMessage - opsHeaderSize
)

type opsOpCode uint16

const (
	opZFSClone    opsOpCode = 1
	opZFSDestroy  opsOpCode = 2
	opTapCreate   opsOpCode = 3
	opTapUp       opsOpCode = 4
	opTapDelete   opsOpCode = 5
	opSetupJail   opsOpCode = 6
	opStartJailer opsOpCode = 7
	opChown       opsOpCode = 8
	opMknodBlock  opsOpCode = 9
)

type opsStatus uint16

const (
	opsStatusOK               opsStatus = 0
	opsStatusInvalidRequest   opsStatus = 1
	opsStatusValidationFailed opsStatus = 2
	opsStatusOperationFailed  opsStatus = 3
	opsStatusInternalError    opsStatus = 4
)

func opsEncodeRequest(buf []byte, op opsOpCode, requestID uint64, payload []byte) int {
	if len(payload) > opsMaxPayload {
		panic("ops payload too large")
	}
	binary.LittleEndian.PutUint32(buf[0:4], opsRequestMagic)
	binary.LittleEndian.PutUint16(buf[4:6], opsProtoVersion)
	binary.LittleEndian.PutUint16(buf[6:8], uint16(op))
	binary.LittleEndian.PutUint64(buf[8:16], requestID)
	binary.LittleEndian.PutUint16(buf[16:18], uint16(len(payload)))
	copy(buf[opsHeaderSize:], payload)
	return opsHeaderSize + len(payload)
}

func opsDecodeResponse(msg []byte) (status opsStatus, requestID uint64, payload []byte, err error) {
	if len(msg) < opsHeaderSize {
		return 0, 0, nil, fmt.Errorf("ops response truncated: %d bytes", len(msg))
	}
	magic := binary.LittleEndian.Uint32(msg[0:4])
	if magic != opsResponseMagic {
		return 0, 0, nil, fmt.Errorf("ops response invalid magic: 0x%08x", magic)
	}
	version := binary.LittleEndian.Uint16(msg[4:6])
	if version != opsProtoVersion {
		return 0, 0, nil, fmt.Errorf("ops response unsupported version: %d", version)
	}
	status = opsStatus(binary.LittleEndian.Uint16(msg[6:8]))
	requestID = binary.LittleEndian.Uint64(msg[8:16])
	payloadLen := int(binary.LittleEndian.Uint16(msg[16:18]))
	if opsHeaderSize+payloadLen > len(msg) {
		return 0, 0, nil, fmt.Errorf("ops response payload truncated")
	}
	payload = msg[opsHeaderSize : opsHeaderSize+payloadLen]
	return status, requestID, payload, nil
}

// opsPayloadBuilder builds length-prefixed string + uint32 payloads.
type opsPayloadBuilder struct {
	buf [opsMaxPayload]byte
	pos int
}

func (b *opsPayloadBuilder) writeString(s string) {
	binary.LittleEndian.PutUint16(b.buf[b.pos:b.pos+2], uint16(len(s)))
	b.pos += 2
	copy(b.buf[b.pos:], s)
	b.pos += len(s)
}

func (b *opsPayloadBuilder) writeU32(v uint32) {
	binary.LittleEndian.PutUint32(b.buf[b.pos:b.pos+4], v)
	b.pos += 4
}

func (b *opsPayloadBuilder) bytes() []byte {
	return b.buf[:b.pos]
}
