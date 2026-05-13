# Connect And Protobuf

Connect/protobuf contracts belong here when the protocol shape is genuinely
RPC-oriented: streaming, binary payloads, privileged substrate control, or tight
internal service protocols where resource-oriented HTTP/JSON would add
accidental complexity.

Public control-plane APIs remain Smithy-first. Shared protobuf messages may
reference the same domain vocabulary.
