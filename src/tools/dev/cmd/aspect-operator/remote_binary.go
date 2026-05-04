package main

import (
	"os"

	opruntime "github.com/verself/operator-runtime/runtime"
)

func runRemoteBazelExecutable(rt *opruntime.Runtime, target, binRel, prefix, runAsUser string, args []string) error {
	localPath, err := buildBazelBinary(rt.Ctx, rt.RepoRoot, target, binRel)
	if err != nil {
		return err
	}
	remotePath, err := rt.SSH.UploadExecutable(rt.Ctx, localPath, prefix)
	if err != nil {
		return err
	}
	defer func() { _ = rt.SSH.RemoveRemotePath(contextWithoutCancel(rt.Ctx), remotePath) }()
	remoteArgs := append([]string{remotePath}, args...)
	return rt.SSH.RunArgv(rt.Ctx, runAsUser, remoteArgs, nil, os.Stdout, os.Stderr)
}
