package notifications

import "fmt"

const (
	WorkflowRecipientLimit        = 100
	WorkflowRecipientChannelLimit = WorkflowRecipientLimit * 2
	workflowRecipientLimitInt32   = int32(WorkflowRecipientLimit)
	workflowChannelLimitInt32     = int32(WorkflowRecipientChannelLimit)
)

func int32FromInt(value int, field string) int32 {
	const (
		minInt32 = -1 << 31
		maxInt32 = 1<<31 - 1
	)
	if value < minInt32 || value > maxInt32 {
		panic(fmt.Sprintf("%s exceeds int32 range: %d", field, value))
	}
	return int32(value) // #nosec G115 -- value is checked against the int32 range above.
}

func workflowCountFromInt(value int, field string) (uint16, error) {
	if value < 0 || value > WorkflowRecipientLimit {
		return 0, fmt.Errorf("%s exceeds workflow recipient limit: %d", field, value)
	}
	return uint16(value), nil // #nosec G115 -- value is checked against the workflow recipient limit above.
}

func workflowCountFromInt32(value int32, field string) (uint16, error) {
	if value < 0 || value > workflowRecipientLimitInt32 {
		return 0, fmt.Errorf("%s exceeds workflow recipient limit: %d", field, value)
	}
	return uint16(value), nil // #nosec G115 -- value is checked against the workflow recipient limit above.
}

func workflowChannelCountFromInt32(value int32, field string) (uint16, error) {
	if value < 0 || value > workflowChannelLimitInt32 {
		return 0, fmt.Errorf("%s exceeds workflow channel limit: %d", field, value)
	}
	return uint16(value), nil // #nosec G115 -- value is checked against the workflow channel limit above.
}
