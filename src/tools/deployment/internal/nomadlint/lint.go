package nomadlint

import (
	"fmt"
	"sort"
	"strings"

	"github.com/hashicorp/nomad/api"
)

const (
	SeverityError = "error"
	SeverityWarn  = "warn"

	GroupKindService         = "service"
	GroupKindWorker          = "worker"
	GroupKindBatch           = "batch"
	GroupKindMigration       = "migration"
	GroupKindSubstrateDaemon = "substrate-daemon"

	metaGroupKind               = "verself_group_kind"
	metaTemplateFallback        = "verself_template_fallback"
	metaTemplateFallbackReason  = "verself_template_fallback_reason"
	metaAllowMigrationPrestart  = "verself_allow_prestart_migration"
	metaAllowLifecycleBootstrap = "verself_allow_lifecycle_bootstrap"
)

type Finding struct {
	RuleID    string
	Severity  string
	JobID     string
	TaskGroup string
	Task      string
	Message   string
	Evidence  string
}

type Result struct {
	Findings []Finding
}

type NodeCapacity struct {
	NodeID   string
	Name     string
	Class    string
	CPU      int64
	MemoryMB int64
	DiskMB   int64
}

func (r Result) Failed() bool {
	for _, finding := range r.Findings {
		if finding.Severity == SeverityError {
			return true
		}
	}
	return false
}

func (r Result) ErrorCount() int {
	count := 0
	for _, finding := range r.Findings {
		if finding.Severity == SeverityError {
			count++
		}
	}
	return count
}

func (r Result) WarningCount() int {
	count := 0
	for _, finding := range r.Findings {
		if finding.Severity == SeverityWarn {
			count++
		}
	}
	return count
}

func Lint(job *api.Job) Result {
	return lint(job, nil)
}

func LintWithCapacity(job *api.Job, capacity []NodeCapacity) Result {
	return lint(job, capacity)
}

func lint(job *api.Job, capacity []NodeCapacity) Result {
	result := Result{}
	if job == nil || job.ID == nil || *job.ID == "" {
		return Result{Findings: []Finding{{
			RuleID:   "nomad.job.id_required",
			Severity: SeverityError,
			Message:  "Nomad job ID is required",
		}}}
	}
	jobID := *job.ID
	for _, group := range job.TaskGroups {
		if group == nil {
			continue
		}
		groupName := stringValue(group.Name)
		kind := strings.TrimSpace(group.Meta[metaGroupKind])
		if kind == "" {
			result.add("nomad.group.kind_required", SeverityError, jobID, groupName, "", "task group must declare verself_group_kind", "")
			continue
		}
		if !validKind(kind) {
			result.add("nomad.group.kind_invalid", SeverityError, jobID, groupName, "", "task group has unsupported verself_group_kind", kind)
		}
		lintDeadlineOrder(&result, jobID, groupName, group.Update)
		lintTemplateFallbacks(&result, jobID, groupName, group)
		lintResourceCapacity(&result, job, jobID, groupName, group, capacity)
		switch kind {
		case GroupKindService:
			lintServiceGroup(&result, jobID, groupName, group)
		case GroupKindWorker:
			lintWorkerGroup(&result, jobID, groupName, group)
		case GroupKindBatch:
			lintBatchGroup(&result, jobID, groupName, job, group)
		case GroupKindMigration:
			lintMigrationGroup(&result, jobID, groupName, job, group)
		case GroupKindSubstrateDaemon:
			lintSubstrateDaemonGroup(&result, jobID, groupName, group)
		}
	}
	sort.SliceStable(result.Findings, func(i, j int) bool {
		a := result.Findings[i]
		b := result.Findings[j]
		if a.Severity != b.Severity {
			return a.Severity < b.Severity
		}
		if a.JobID != b.JobID {
			return a.JobID < b.JobID
		}
		if a.TaskGroup != b.TaskGroup {
			return a.TaskGroup < b.TaskGroup
		}
		if a.Task != b.Task {
			return a.Task < b.Task
		}
		return a.RuleID < b.RuleID
	})
	return result
}

func lintServiceGroup(result *Result, jobID, groupName string, group *api.TaskGroup) {
	services := groupServices(group)
	if len(services) == 0 {
		result.add("nomad.health.service_registration_required", SeverityError, jobID, groupName, "", "service group must register at least one Nomad service", "")
	}
	for _, service := range services {
		if len(service.checks) == 0 {
			result.add("nomad.health.service_requires_checks", SeverityError, jobID, groupName, service.task, "service registrations must carry explicit health checks", service.name)
		}
	}
	if updateHealth(group.Update) == "" {
		result.add("nomad.health.service_requires_explicit_update", SeverityError, jobID, groupName, "", "service groups must declare update health_check = checks", "")
	}
	if updateHealth(group.Update) != "" && updateHealth(group.Update) != "checks" {
		result.add("nomad.health.service_requires_checks_update", SeverityError, jobID, groupName, "", "service group update health_check must use checks", updateHealth(group.Update))
	}
	for _, task := range group.Tasks {
		if task == nil || task.Lifecycle == nil || task.Lifecycle.Sidecar {
			continue
		}
		if task.Lifecycle.Hook == api.TaskLifecycleHookPrestart && strings.TrimSpace(group.Meta[metaAllowMigrationPrestart]) == "true" {
			continue
		}
		if strings.TrimSpace(group.Meta[metaAllowLifecycleBootstrap]) == "true" {
			continue
		}
		result.add("nomad.group.mixed_service_and_one_shot", SeverityError, jobID, groupName, task.Name, "service groups must not mix long-lived service rollout with one-shot lifecycle work unless modeled as a migration", task.Lifecycle.Hook)
	}
}

func lintWorkerGroup(result *Result, jobID, groupName string, group *api.TaskGroup) {
	if len(groupServices(group)) > 0 {
		result.add("nomad.group.worker_has_service_registration", SeverityError, jobID, groupName, "", "worker groups must not register services", "")
	}
	health := updateHealth(group.Update)
	if health == "" {
		result.add("nomad.health.worker_requires_explicit_update", SeverityError, jobID, groupName, "", "worker groups must declare update health_check", "")
	}
	if health == "task_states" && hasEphemeralLifecycle(group) {
		result.add("nomad.health.task_states_with_ephemeral_tasks", SeverityError, jobID, groupName, "", "task_states health is unsafe with non-sidecar lifecycle tasks", "")
	}
}

func lintBatchGroup(result *Result, jobID, groupName string, job *api.Job, group *api.TaskGroup) {
	if jobType(job) != "batch" && jobType(job) != "sysbatch" {
		result.add("nomad.group.batch_requires_batch_job", SeverityError, jobID, groupName, "", "batch group intent requires a batch or sysbatch job", jobType(job))
	}
	if len(groupServices(group)) > 0 {
		result.add("nomad.group.batch_has_service_registration", SeverityError, jobID, groupName, "", "batch groups must not register services", "")
	}
}

func lintMigrationGroup(result *Result, jobID, groupName string, job *api.Job, group *api.TaskGroup) {
	lintBatchGroup(result, jobID, groupName, job, group)
}

func lintSubstrateDaemonGroup(result *Result, jobID, groupName string, group *api.TaskGroup) {
	health := updateHealth(group.Update)
	if health == "" {
		result.add("nomad.health.substrate_daemon_requires_update", SeverityError, jobID, groupName, "", "substrate daemon groups must declare update health_check", "")
	}
}

func lintResourceCapacity(result *Result, job *api.Job, jobID, groupName string, group *api.TaskGroup, capacity []NodeCapacity) {
	if len(capacity) == 0 {
		return
	}
	requiredClasses := requiredNodeClasses(job, group)
	best, ok := bestCapacity(capacity, requiredClasses)
	if !ok {
		result.add("nomad.resources.node_class_missing", SeverityError, jobID, groupName, "", "no schedulable nodes match the task group's node class constraint", strings.Join(requiredClasses, ","))
		return
	}
	requested := groupReservation(group)
	if best.CPU > 0 && requested.CPU > best.CPU {
		result.add("nomad.resources.cpu_exceeds_node_class", SeverityError, jobID, groupName, "", "task group CPU reservation exceeds current node class capacity", fmt.Sprintf("requested=%d available=%d class=%s", requested.CPU, best.CPU, best.Class))
	}
	if best.MemoryMB > 0 && requested.MemoryMB > best.MemoryMB {
		result.add("nomad.resources.memory_exceeds_node_class", SeverityError, jobID, groupName, "", "task group memory reservation exceeds current node class capacity", fmt.Sprintf("requested=%d available=%d class=%s", requested.MemoryMB, best.MemoryMB, best.Class))
	}
	if best.DiskMB > 0 && requested.DiskMB > best.DiskMB {
		result.add("nomad.resources.disk_exceeds_node_class", SeverityError, jobID, groupName, "", "task group disk reservation exceeds current node class capacity", fmt.Sprintf("requested=%d available=%d class=%s", requested.DiskMB, best.DiskMB, best.Class))
	}
}

type resourceReservation struct {
	CPU      int64
	MemoryMB int64
	DiskMB   int64
}

func groupReservation(group *api.TaskGroup) resourceReservation {
	defaults := api.DefaultResources()
	var reservation resourceReservation
	for _, task := range group.Tasks {
		if task == nil {
			continue
		}
		resources := task.Resources
		if resources == nil {
			resources = defaults
		}
		reservation.CPU += intPtr(resources.CPU, intPtr(defaults.CPU, 100))
		reservation.MemoryMB += intPtr(resources.MemoryMB, intPtr(defaults.MemoryMB, 300))
		reservation.DiskMB += intPtr(resources.DiskMB, 0)
	}
	if group.EphemeralDisk == nil {
		reservation.DiskMB += 300
	} else {
		reservation.DiskMB += intPtr(group.EphemeralDisk.SizeMB, 300)
	}
	return reservation
}

func requiredNodeClasses(job *api.Job, group *api.TaskGroup) []string {
	var classes []string
	classes = appendRequiredNodeClasses(classes, job.Constraints)
	classes = appendRequiredNodeClasses(classes, group.Constraints)
	sort.Strings(classes)
	return compactStrings(classes)
}

func appendRequiredNodeClasses(classes []string, constraints []*api.Constraint) []string {
	for _, constraint := range constraints {
		if class := requiredNodeClass(constraint); class != "" {
			classes = append(classes, class)
		}
	}
	return classes
}

func requiredNodeClass(constraint *api.Constraint) string {
	if constraint == nil {
		return ""
	}
	target := strings.Trim(constraint.LTarget, "${}")
	if target != "node.class" {
		return ""
	}
	switch strings.TrimSpace(constraint.Operand) {
	case "", "=", "==", "is":
		return strings.TrimSpace(constraint.RTarget)
	default:
		return ""
	}
}

func bestCapacity(capacity []NodeCapacity, requiredClasses []string) (NodeCapacity, bool) {
	classAllowed := map[string]struct{}{}
	for _, class := range requiredClasses {
		classAllowed[class] = struct{}{}
	}
	var best NodeCapacity
	var ok bool
	for _, candidate := range capacity {
		if len(classAllowed) != 0 {
			if _, allowed := classAllowed[candidate.Class]; !allowed {
				continue
			}
		}
		best.CPU = maxInt64(best.CPU, candidate.CPU)
		best.MemoryMB = maxInt64(best.MemoryMB, candidate.MemoryMB)
		best.DiskMB = maxInt64(best.DiskMB, candidate.DiskMB)
		ok = true
	}
	if len(requiredClasses) == 0 {
		best.Class = "any"
	} else {
		best.Class = strings.Join(requiredClasses, ",")
	}
	return best, ok
}

func compactStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := values[:0]
	for _, value := range values {
		if len(out) == 0 || out[len(out)-1] != value {
			out = append(out, value)
		}
	}
	return out
}

func lintDeadlineOrder(result *Result, jobID, groupName string, update *api.UpdateStrategy) {
	if update == nil || update.HealthyDeadline == nil || update.ProgressDeadline == nil {
		return
	}
	if *update.ProgressDeadline <= *update.HealthyDeadline {
		result.add(
			"nomad.update.progress_deadline_order",
			SeverityError,
			jobID,
			groupName,
			"",
			"progress_deadline must be greater than healthy_deadline",
			fmt.Sprintf("healthy_deadline=%s progress_deadline=%s", update.HealthyDeadline.String(), update.ProgressDeadline.String()),
		)
	}
}

func lintTemplateFallbacks(result *Result, jobID, groupName string, group *api.TaskGroup) {
	for _, task := range group.Tasks {
		if task == nil {
			continue
		}
		for _, tmpl := range task.Templates {
			if tmpl == nil || tmpl.EmbeddedTmpl == nil || !strings.Contains(*tmpl.EmbeddedTmpl, "127.0.0.1:1") {
				continue
			}
			if strings.TrimSpace(group.Meta[metaTemplateFallback]) == "blackhole" && strings.TrimSpace(group.Meta[metaTemplateFallbackReason]) != "" {
				continue
			}
			result.add("nomad.template.loopback_blackhole_fallback", SeverityError, jobID, groupName, task.Name, "127.0.0.1:1 template fallback must be intentionally modeled", stringValue(tmpl.DestPath))
		}
	}
}

type serviceRef struct {
	task   string
	name   string
	checks []api.ServiceCheck
}

func groupServices(group *api.TaskGroup) []serviceRef {
	var out []serviceRef
	for _, service := range group.Services {
		if service == nil {
			continue
		}
		out = append(out, serviceRef{name: service.Name, checks: service.Checks})
	}
	for _, task := range group.Tasks {
		if task == nil {
			continue
		}
		for _, service := range task.Services {
			if service == nil {
				continue
			}
			out = append(out, serviceRef{task: task.Name, name: service.Name, checks: service.Checks})
		}
	}
	return out
}

func hasEphemeralLifecycle(group *api.TaskGroup) bool {
	for _, task := range group.Tasks {
		if task != nil && task.Lifecycle != nil && !task.Lifecycle.Sidecar {
			return true
		}
	}
	return false
}

func updateHealth(update *api.UpdateStrategy) string {
	if update == nil || update.HealthCheck == nil {
		return ""
	}
	return strings.TrimSpace(*update.HealthCheck)
}

func jobType(job *api.Job) string {
	if job == nil || job.Type == nil || strings.TrimSpace(*job.Type) == "" {
		return "service"
	}
	return strings.TrimSpace(*job.Type)
}

func validKind(kind string) bool {
	switch kind {
	case GroupKindService, GroupKindWorker, GroupKindBatch, GroupKindMigration, GroupKindSubstrateDaemon:
		return true
	default:
		return false
	}
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func intPtr(value *int, fallback int64) int64 {
	if value == nil {
		return fallback
	}
	return int64(*value)
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func (r *Result) add(ruleID, severity, jobID, taskGroup, task, message, evidence string) {
	r.Findings = append(r.Findings, Finding{
		RuleID:    ruleID,
		Severity:  severity,
		JobID:     jobID,
		TaskGroup: taskGroup,
		Task:      task,
		Message:   message,
		Evidence:  evidence,
	})
}
