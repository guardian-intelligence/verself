package releaseworkflow

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/spiffe/go-spiffe/v2/workloadapi"
	"github.com/verself/temporal-platform/sdkclient"
	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/api/serviceerror"
	tclient "go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"
)

const (
	DefaultNamespace = "distribution-service"
	DefaultTaskQueue = "distribution-service.release-v1"

	NightlyReleaseWorkflowName          = "verself.distribution.release.nightly.v1"
	ScheduledNightlyReleaseWorkflowName = "verself.distribution.release.nightly-from-ref.v1"
	ReleaseCandidateWorkflowName        = "verself.distribution.release.rc.v1"
	StableReleaseWorkflowName           = "verself.distribution.release.stable.v1"

	defaultWorkflowExecutionTimeout = 6 * time.Hour
	defaultCatchupWindow            = 15 * time.Minute
)

const (
	ChannelNightly = "nightly"
	ChannelRC      = "rc"
	ChannelStable  = "stable"

	PackageMksk = "mksk"
)

var (
	ErrInvalidInput = errors.New("distribution release workflow: invalid input")

	packagePattern      = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
	gitCommitPattern    = regexp.MustCompile(`^[0-9a-f]{40}$`)
	stableSemVerPattern = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)
	rcSemVerPattern     = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+-rc\.[0-9]+$`)
)

type Config struct {
	HostPort  string
	Namespace string
	TaskQueue string
	Source    *workloadapi.X509Source
}

type Client struct {
	temporal  tclient.Client
	namespace string
	taskQueue string
}

type PinnedSource struct {
	Ref    string `json:"source_ref"`
	Commit string `json:"source_commit"`
}

type FloatingSource struct {
	Ref string `json:"source_ref"`
}

type NightlyReleaseInput struct {
	Package string       `json:"package"`
	Source  PinnedSource `json:"source"`
}

type ScheduledNightlyReleaseInput struct {
	Package string         `json:"package"`
	Source  FloatingSource `json:"source"`
}

type ReleaseCandidateInput struct {
	Package string       `json:"package"`
	Version string       `json:"version"`
	Source  PinnedSource `json:"source"`
}

type StableReleaseInput struct {
	Package string       `json:"package"`
	Version string       `json:"version"`
	Source  PinnedSource `json:"source"`
}

type NightlyScheduleRequest struct {
	Package   string
	SourceRef string
	HourUTC   int
	MinuteUTC int
	Jitter    time.Duration
	Paused    bool
}

type ReleaseRun struct {
	WorkflowID       string
	RunID            string
	AlreadyCompleted bool
	Result           ReleaseWorkflowResult
}

type ReleaseSchedule struct {
	ScheduleID string
	Namespace  string
	TaskQueue  string
}

func Dial(ctx context.Context, cfg Config) (*Client, error) {
	hostPort := strings.TrimSpace(cfg.HostPort)
	if hostPort == "" {
		return nil, fmt.Errorf("%w: temporal host port is required", ErrInvalidInput)
	}
	if cfg.Source == nil {
		return nil, fmt.Errorf("%w: spiffe source is required", ErrInvalidInput)
	}
	namespace := strings.TrimSpace(cfg.Namespace)
	if namespace == "" {
		namespace = DefaultNamespace
	}
	taskQueue := strings.TrimSpace(cfg.TaskQueue)
	if taskQueue == "" {
		taskQueue = DefaultTaskQueue
	}
	temporalClient, err := sdkclient.NewWorkflowClient(sdkclient.Config{
		HostPort: hostPort,
	}, namespace, cfg.Source, "distribution-service-release-sdk")
	if err != nil {
		return nil, fmt.Errorf("dial distribution release temporal client: %w", err)
	}
	return NewClient(temporalClient, namespace, taskQueue)
}

func NewClient(temporalClient tclient.Client, namespace string, taskQueue string) (*Client, error) {
	if temporalClient == nil {
		return nil, fmt.Errorf("%w: temporal client is required", ErrInvalidInput)
	}
	namespace = strings.TrimSpace(namespace)
	if namespace == "" {
		namespace = DefaultNamespace
	}
	taskQueue = strings.TrimSpace(taskQueue)
	if taskQueue == "" {
		taskQueue = DefaultTaskQueue
	}
	return &Client{
		temporal:  temporalClient,
		namespace: namespace,
		taskQueue: taskQueue,
	}, nil
}

func (c *Client) Close() {
	if c == nil || c.temporal == nil {
		return
	}
	c.temporal.Close()
}

func (c *Client) StartNightly(ctx context.Context, input NightlyReleaseInput) (ReleaseRun, error) {
	if err := validateNightlyInput(input); err != nil {
		return ReleaseRun{}, err
	}
	workflowID := releaseWorkflowID(input.Package, ChannelNightly, "", input.Source.Commit)
	return c.execute(ctx, workflowID, NightlyReleaseWorkflowName, input)
}

func (c *Client) StartReleaseCandidate(ctx context.Context, input ReleaseCandidateInput) (ReleaseRun, error) {
	if err := validateReleaseCandidateInput(input); err != nil {
		return ReleaseRun{}, err
	}
	workflowID := releaseWorkflowID(input.Package, ChannelRC, input.Version, input.Source.Commit)
	return c.execute(ctx, workflowID, ReleaseCandidateWorkflowName, input)
}

func (c *Client) StartStable(ctx context.Context, input StableReleaseInput) (ReleaseRun, error) {
	if err := validateStableInput(input); err != nil {
		return ReleaseRun{}, err
	}
	workflowID := releaseWorkflowID(input.Package, ChannelStable, input.Version, input.Source.Commit)
	return c.execute(ctx, workflowID, StableReleaseWorkflowName, input)
}

func (c *Client) EnsureNightlySchedule(ctx context.Context, req NightlyScheduleRequest) (ReleaseSchedule, error) {
	if c == nil || c.temporal == nil {
		return ReleaseSchedule{}, fmt.Errorf("%w: temporal client is required", ErrInvalidInput)
	}
	if err := validateNightlyScheduleRequest(req); err != nil {
		return ReleaseSchedule{}, err
	}
	opts := c.nightlyScheduleOptions(req)
	handle, err := c.temporal.ScheduleClient().Create(ctx, opts)
	if err == nil {
		return ReleaseSchedule{ScheduleID: handle.GetID(), Namespace: c.namespace, TaskQueue: c.taskQueue}, nil
	}
	if !errors.Is(err, temporal.ErrScheduleAlreadyRunning) {
		return ReleaseSchedule{}, fmt.Errorf("create temporal nightly schedule %s: %w", opts.ID, err)
	}
	handle = c.temporal.ScheduleClient().GetHandle(ctx, opts.ID)
	if err := handle.Update(ctx, tclient.ScheduleUpdateOptions{
		DoUpdate: func(tclient.ScheduleUpdateInput) (*tclient.ScheduleUpdate, error) {
			return &tclient.ScheduleUpdate{
				Schedule: scheduleFromOptions(opts),
			}, nil
		},
	}); err != nil {
		return ReleaseSchedule{}, fmt.Errorf("update temporal nightly schedule %s: %w", opts.ID, err)
	}
	return ReleaseSchedule{ScheduleID: opts.ID, Namespace: c.namespace, TaskQueue: c.taskQueue}, nil
}

func (c *Client) execute(ctx context.Context, workflowID string, workflowName string, input interface{}) (ReleaseRun, error) {
	if c == nil || c.temporal == nil {
		return ReleaseRun{}, fmt.Errorf("%w: temporal client is required", ErrInvalidInput)
	}
	run, err := c.temporal.ExecuteWorkflow(ctx, tclient.StartWorkflowOptions{
		ID:                                       workflowID,
		TaskQueue:                                c.taskQueue,
		WorkflowExecutionTimeout:                 defaultWorkflowExecutionTimeout,
		WorkflowIDConflictPolicy:                 enumspb.WORKFLOW_ID_CONFLICT_POLICY_FAIL,
		WorkflowIDReusePolicy:                    enumspb.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE_FAILED_ONLY,
		WorkflowExecutionErrorWhenAlreadyStarted: true,
		Memo: map[string]interface{}{
			"package":     memoPackage(input),
			"channel":     memoChannel(workflowName),
			"workflow_id": workflowID,
		},
	}, workflowName, input)
	if err != nil {
		var alreadyStarted *serviceerror.WorkflowExecutionAlreadyStarted
		if errors.As(err, &alreadyStarted) {
			return c.completedReleaseRun(ctx, workflowID, alreadyStarted.RunId, err)
		}
		return ReleaseRun{}, fmt.Errorf("start temporal release workflow %s: %w", workflowID, err)
	}
	var result ReleaseWorkflowResult
	if err := run.Get(ctx, &result); err != nil {
		return ReleaseRun{}, fmt.Errorf("wait for temporal release workflow %s: %w", workflowID, err)
	}
	return ReleaseRun{
		WorkflowID: run.GetID(),
		RunID:      run.GetRunID(),
		Result:     result,
	}, nil
}

func (c *Client) completedReleaseRun(ctx context.Context, workflowID string, runID string, original error) (ReleaseRun, error) {
	desc, err := c.temporal.DescribeWorkflowExecution(ctx, workflowID, runID)
	if err != nil {
		return ReleaseRun{}, fmt.Errorf("describe existing temporal release workflow %s: %w", workflowID, err)
	}
	info := desc.GetWorkflowExecutionInfo()
	if info == nil || info.GetStatus() != enumspb.WORKFLOW_EXECUTION_STATUS_COMPLETED {
		return ReleaseRun{}, fmt.Errorf("start temporal release workflow %s: %w", workflowID, original)
	}
	var result ReleaseWorkflowResult
	if err := c.temporal.GetWorkflow(ctx, workflowID, runID).Get(ctx, &result); err != nil {
		return ReleaseRun{}, fmt.Errorf("read completed temporal release workflow %s result: %w", workflowID, err)
	}
	return ReleaseRun{
		WorkflowID:       workflowID,
		RunID:            runID,
		AlreadyCompleted: true,
		Result:           result,
	}, nil
}

func (c *Client) nightlyScheduleOptions(req NightlyScheduleRequest) tclient.ScheduleOptions {
	scheduleID := nightlyScheduleID(req.Package)
	input := ScheduledNightlyReleaseInput{
		Package: req.Package,
		Source:  FloatingSource{Ref: strings.TrimSpace(req.SourceRef)},
	}
	return tclient.ScheduleOptions{
		ID: scheduleID,
		Spec: tclient.ScheduleSpec{
			Calendars: []tclient.ScheduleCalendarSpec{{
				Second:     []tclient.ScheduleRange{{Start: 0}},
				Minute:     []tclient.ScheduleRange{{Start: req.MinuteUTC}},
				Hour:       []tclient.ScheduleRange{{Start: req.HourUTC}},
				DayOfMonth: []tclient.ScheduleRange{{Start: 1, End: 31}},
				Month:      []tclient.ScheduleRange{{Start: 1, End: 12}},
				DayOfWeek:  []tclient.ScheduleRange{{Start: 0, End: 6}},
				Comment:    req.Package + " nightly release",
			}},
			TimeZoneName: "UTC",
			Jitter:       req.Jitter,
		},
		Action: &tclient.ScheduleWorkflowAction{
			ID:                       scheduleID + "/dispatch",
			Workflow:                 ScheduledNightlyReleaseWorkflowName,
			Args:                     []interface{}{input},
			TaskQueue:                c.taskQueue,
			WorkflowExecutionTimeout: defaultWorkflowExecutionTimeout,
			Memo: map[string]interface{}{
				"package":     req.Package,
				"channel":     ChannelNightly,
				"schedule_id": scheduleID,
				"source_ref":  strings.TrimSpace(req.SourceRef),
			},
		},
		Overlap:        enumspb.SCHEDULE_OVERLAP_POLICY_SKIP,
		CatchupWindow:  defaultCatchupWindow,
		PauseOnFailure: true,
		Paused:         req.Paused,
		Note:           "managed by distribution-service",
		Memo: map[string]interface{}{
			"package":     req.Package,
			"channel":     ChannelNightly,
			"schedule_id": scheduleID,
			"source_ref":  strings.TrimSpace(req.SourceRef),
		},
	}
}

func scheduleFromOptions(opts tclient.ScheduleOptions) *tclient.Schedule {
	return &tclient.Schedule{
		Action: opts.Action,
		Spec:   &opts.Spec,
		Policy: &tclient.SchedulePolicies{
			Overlap:        opts.Overlap,
			CatchupWindow:  opts.CatchupWindow,
			PauseOnFailure: opts.PauseOnFailure,
		},
		State: &tclient.ScheduleState{
			Paused: opts.Paused,
			Note:   opts.Note,
		},
	}
}

func releaseWorkflowID(packageName string, channel string, version string, sourceCommit string) string {
	parts := []string{"release", normalizeToken(packageName), normalizeToken(channel)}
	if strings.TrimSpace(version) != "" {
		parts = append(parts, normalizeToken(version))
	}
	parts = append(parts, strings.ToLower(strings.TrimSpace(sourceCommit)))
	return strings.Join(parts, "/")
}

func nightlyScheduleID(packageName string) string {
	return "release/" + normalizeToken(packageName) + "/nightly"
}

func validateNightlyInput(input NightlyReleaseInput) error {
	if err := validatePackage(input.Package); err != nil {
		return err
	}
	return validatePinnedSource(input.Source)
}

func validateScheduledNightlyInput(input ScheduledNightlyReleaseInput) error {
	if err := validatePackage(input.Package); err != nil {
		return err
	}
	return validateSourceRef(input.Source.Ref)
}

func validateReleaseCandidateInput(input ReleaseCandidateInput) error {
	if err := validatePackage(input.Package); err != nil {
		return err
	}
	if !rcSemVerPattern.MatchString(strings.TrimSpace(input.Version)) {
		return fmt.Errorf("%w: rc version must look like 0.2.0-rc.1", ErrInvalidInput)
	}
	return validatePinnedSource(input.Source)
}

func validateStableInput(input StableReleaseInput) error {
	if err := validatePackage(input.Package); err != nil {
		return err
	}
	if !stableSemVerPattern.MatchString(strings.TrimSpace(input.Version)) {
		return fmt.Errorf("%w: stable version must look like 0.2.0", ErrInvalidInput)
	}
	return validatePinnedSource(input.Source)
}

func validatePinnedSource(source PinnedSource) error {
	if err := validateSourceRef(source.Ref); err != nil {
		return err
	}
	commit := strings.TrimSpace(source.Commit)
	if commit != strings.ToLower(commit) {
		return fmt.Errorf("%w: source commit must be a full lowercase git sha", ErrInvalidInput)
	}
	if !gitCommitPattern.MatchString(commit) {
		return fmt.Errorf("%w: source commit must be a full lowercase git sha", ErrInvalidInput)
	}
	return nil
}

func validateNightlyScheduleRequest(req NightlyScheduleRequest) error {
	if err := validatePackage(req.Package); err != nil {
		return err
	}
	if err := validateSourceRef(req.SourceRef); err != nil {
		return err
	}
	if req.HourUTC < 0 || req.HourUTC > 23 {
		return fmt.Errorf("%w: schedule hour must be between 0 and 23 UTC", ErrInvalidInput)
	}
	if req.MinuteUTC < 0 || req.MinuteUTC > 59 {
		return fmt.Errorf("%w: schedule minute must be between 0 and 59 UTC", ErrInvalidInput)
	}
	if req.Jitter < 0 {
		return fmt.Errorf("%w: schedule jitter must not be negative", ErrInvalidInput)
	}
	if req.Jitter >= 24*time.Hour {
		return fmt.Errorf("%w: schedule jitter must be less than one day", ErrInvalidInput)
	}
	return nil
}

func validatePackage(packageName string) error {
	_, err := LookupDistributable(packageName)
	return err
}

func validateSourceRef(sourceRef string) error {
	sourceRef = strings.TrimSpace(sourceRef)
	if sourceRef == "" {
		return fmt.Errorf("%w: source ref is required", ErrInvalidInput)
	}
	if strings.ContainsAny(sourceRef, " \t\r\n") {
		return fmt.Errorf("%w: source ref must not contain whitespace", ErrInvalidInput)
	}
	return nil
}

func normalizeToken(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, ".", "-")
	for strings.Contains(value, "--") {
		value = strings.ReplaceAll(value, "--", "-")
	}
	return strings.Trim(value, "-")
}

func memoPackage(input interface{}) string {
	switch v := input.(type) {
	case NightlyReleaseInput:
		return v.Package
	case ReleaseCandidateInput:
		return v.Package
	case StableReleaseInput:
		return v.Package
	default:
		return ""
	}
}

func memoChannel(workflowName string) string {
	switch workflowName {
	case NightlyReleaseWorkflowName:
		return ChannelNightly
	case ReleaseCandidateWorkflowName:
		return ChannelRC
	case StableReleaseWorkflowName:
		return ChannelStable
	default:
		return ""
	}
}
