package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"strconv"
	"strings"
	"syscall"
)

type stringList []string

func (l *stringList) String() string {
	return strings.Join(*l, ",")
}

func (l *stringList) Set(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	*l = append(*l, value)
	return nil
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "nomad-chown-exec: "+err.Error())
		os.Exit(1)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("nomad-chown-exec", flag.ContinueOnError)
	var userName string
	var groupName string
	var paths stringList
	fs.StringVar(&userName, "user", "", "User to run as after ownership repair.")
	fs.StringVar(&groupName, "group", "", "Primary group to run as after ownership repair. Defaults to the user's primary group.")
	fs.Var(&paths, "chown", "File to chown before dropping privileges; repeatable.")
	if err := fs.Parse(args); err != nil {
		return err
	}
	command := fs.Args()
	if len(command) == 0 {
		return fmt.Errorf("command is required after --")
	}
	target, err := lookupTarget(userName, groupName)
	if err != nil {
		return err
	}
	for _, path := range paths {
		if err := os.Chown(path, target.uid, target.gid); err != nil {
			return fmt.Errorf("chown %s: %w", path, err)
		}
	}
	if err := syscall.Setgroups(target.groups); err != nil {
		return fmt.Errorf("set supplementary groups: %w", err)
	}
	if err := syscall.Setgid(target.gid); err != nil {
		return fmt.Errorf("set gid: %w", err)
	}
	if err := syscall.Setuid(target.uid); err != nil {
		return fmt.Errorf("set uid: %w", err)
	}
	binary := command[0]
	if !strings.Contains(binary, "/") {
		resolved, err := exec.LookPath(binary)
		if err != nil {
			return fmt.Errorf("resolve command %s: %w", binary, err)
		}
		binary = resolved
	}
	if err := syscall.Exec(binary, command, os.Environ()); err != nil {
		return fmt.Errorf("exec %s: %w", command[0], err)
	}
	return nil
}

type targetIdentity struct {
	uid    int
	gid    int
	groups []int
}

func lookupTarget(userName, groupName string) (targetIdentity, error) {
	userName = strings.TrimSpace(userName)
	if userName == "" {
		return targetIdentity{}, fmt.Errorf("--user is required")
	}
	u, err := user.Lookup(userName)
	if err != nil {
		return targetIdentity{}, fmt.Errorf("lookup user %s: %w", userName, err)
	}
	uid, err := parseID("uid", u.Uid)
	if err != nil {
		return targetIdentity{}, err
	}
	gidRaw := u.Gid
	if strings.TrimSpace(groupName) != "" {
		g, err := user.LookupGroup(groupName)
		if err != nil {
			return targetIdentity{}, fmt.Errorf("lookup group %s: %w", groupName, err)
		}
		gidRaw = g.Gid
	}
	gid, err := parseID("gid", gidRaw)
	if err != nil {
		return targetIdentity{}, err
	}
	groups := []int{gid}
	rawGroups, err := u.GroupIds()
	if err != nil {
		return targetIdentity{}, fmt.Errorf("lookup supplementary groups for %s: %w", userName, err)
	}
	for _, raw := range rawGroups {
		groupID, err := parseID("supplementary gid", raw)
		if err != nil {
			return targetIdentity{}, err
		}
		if groupID != gid {
			groups = append(groups, groupID)
		}
	}
	return targetIdentity{uid: uid, gid: gid, groups: groups}, nil
}

func parseID(label, raw string) (int, error) {
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return 0, fmt.Errorf("parse %s %q: %w", label, raw, err)
	}
	if value < 0 {
		return 0, fmt.Errorf("%s must be non-negative", label)
	}
	return value, nil
}
