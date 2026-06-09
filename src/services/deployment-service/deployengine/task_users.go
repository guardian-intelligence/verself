package deployengine

import (
	"context"
	"fmt"
	"os/user"
	"path"
	"strconv"
	"strings"

	"github.com/hashicorp/nomad/api"
)

type TaskUserIdentity struct {
	UID int
	GID int
}

type TaskUserResolver func(context.Context, string) (TaskUserIdentity, error)

func localTaskUserResolver(_ context.Context, name string) (TaskUserIdentity, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return TaskUserIdentity{}, fmt.Errorf("task user is required")
	}
	found, err := user.Lookup(name)
	if err != nil {
		return TaskUserIdentity{}, fmt.Errorf("lookup task user %q: %w", name, err)
	}
	uid, err := parseUserID(found.Uid, "uid", name)
	if err != nil {
		return TaskUserIdentity{}, err
	}
	gid, err := parseUserID(found.Gid, "gid", name)
	if err != nil {
		return TaskUserIdentity{}, err
	}
	return TaskUserIdentity{UID: uid, GID: gid}, nil
}

func parseUserID(raw, field, userName string) (int, error) {
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return 0, fmt.Errorf("parse %s for task user %q: %w", field, userName, err)
	}
	if value < 0 {
		return 0, fmt.Errorf("%s for task user %q must be non-negative", field, userName)
	}
	return value, nil
}

func applyTaskSecretTemplateOwnership(ctx context.Context, job *api.Job, resolver TaskUserResolver) error {
	if job == nil {
		return nil
	}
	if resolver == nil {
		resolver = localTaskUserResolver
	}
	cache := map[string]TaskUserIdentity{}
	for _, group := range job.TaskGroups {
		if group == nil {
			continue
		}
		for _, task := range group.Tasks {
			if task == nil {
				continue
			}
			for _, tmpl := range task.Templates {
				if tmpl == nil || !isSecretTemplateDestination(tmpl.DestPath) {
					continue
				}
				taskUser := strings.TrimSpace(task.User)
				if taskUser == "" {
					return fmt.Errorf("job %q group %q task %q template %q renders into secrets but task.user is empty", jobID(job), taskGroupName(group), task.Name, templateDestination(tmpl))
				}
				identity, ok := cache[taskUser]
				if !ok {
					resolved, err := resolver(ctx, taskUser)
					if err != nil {
						return fmt.Errorf("job %q group %q task %q resolve task user %q: %w", jobID(job), taskGroupName(group), task.Name, taskUser, err)
					}
					if resolved.UID < 0 || resolved.GID < 0 {
						return fmt.Errorf("job %q group %q task %q user %q resolved to invalid uid=%d gid=%d", jobID(job), taskGroupName(group), task.Name, taskUser, resolved.UID, resolved.GID)
					}
					identity = resolved
					cache[taskUser] = identity
				}
				if err := setTemplateOwnership(job, group, task, tmpl, identity); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func applyPodmanTaskUserIDs(ctx context.Context, job *api.Job, resolver TaskUserResolver) error {
	if job == nil {
		return nil
	}
	if resolver == nil {
		resolver = localTaskUserResolver
	}
	cache := map[string]TaskUserIdentity{}
	for _, group := range job.TaskGroups {
		if group == nil {
			continue
		}
		for _, task := range group.Tasks {
			if task == nil || task.Driver != "podman" {
				continue
			}
			taskUser := strings.TrimSpace(task.User)
			if taskUser == "" || numericTaskUser(taskUser) {
				continue
			}
			identity, ok := cache[taskUser]
			if !ok {
				resolved, err := resolver(ctx, taskUser)
				if err != nil {
					return fmt.Errorf("job %q group %q task %q resolve task user %q: %w", jobID(job), taskGroupName(group), task.Name, taskUser, err)
				}
				if resolved.UID < 0 || resolved.GID < 0 {
					return fmt.Errorf("job %q group %q task %q user %q resolved to invalid uid=%d gid=%d", jobID(job), taskGroupName(group), task.Name, taskUser, resolved.UID, resolved.GID)
				}
				identity = resolved
				cache[taskUser] = identity
			}
			task.User = strconv.Itoa(identity.UID) + ":" + strconv.Itoa(identity.GID)
		}
	}
	return nil
}

func numericTaskUser(value string) bool {
	userPart, groupPart, hasGroup := strings.Cut(value, ":")
	if _, err := strconv.Atoi(userPart); err != nil {
		return false
	}
	if !hasGroup {
		return true
	}
	_, err := strconv.Atoi(groupPart)
	return err == nil
}

func isSecretTemplateDestination(dest *string) bool {
	if dest == nil {
		return false
	}
	cleaned := path.Clean(strings.ReplaceAll(strings.TrimSpace(*dest), "\\", "/"))
	return cleaned == "secrets" || strings.HasPrefix(cleaned, "secrets/")
}

func setTemplateOwnership(job *api.Job, group *api.TaskGroup, task *api.Task, tmpl *api.Template, identity TaskUserIdentity) error {
	destination := templateDestination(tmpl)
	if tmpl.Uid != nil && *tmpl.Uid != identity.UID {
		return fmt.Errorf("job %q group %q task %q template %q has uid=%d but task user resolves to uid=%d", jobID(job), taskGroupName(group), task.Name, destination, *tmpl.Uid, identity.UID)
	}
	if tmpl.Gid != nil && *tmpl.Gid != identity.GID {
		return fmt.Errorf("job %q group %q task %q template %q has gid=%d but task user resolves to gid=%d", jobID(job), taskGroupName(group), task.Name, destination, *tmpl.Gid, identity.GID)
	}
	if tmpl.Uid == nil {
		tmpl.Uid = intPtr(identity.UID)
	}
	if tmpl.Gid == nil {
		tmpl.Gid = intPtr(identity.GID)
	}
	return nil
}

func jobID(job *api.Job) string {
	if job == nil || job.ID == nil {
		return ""
	}
	return *job.ID
}

func taskGroupName(group *api.TaskGroup) string {
	if group == nil || group.Name == nil {
		return ""
	}
	return *group.Name
}

func templateDestination(tmpl *api.Template) string {
	if tmpl == nil || tmpl.DestPath == nil {
		return ""
	}
	return *tmpl.DestPath
}

func intPtr(value int) *int {
	return &value
}
