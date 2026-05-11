package bazeltelemetry

import "strings"

type LabelParts struct {
	Repository string
	Package    string
	RuleName   string
	BuildFile  string
}

func ParseLabel(label string) LabelParts {
	label = strings.TrimSpace(label)
	if label == "" {
		return LabelParts{}
	}
	if strings.HasPrefix(label, "@@") {
		rest := strings.TrimPrefix(label, "@@")
		repo, pkgRule, _ := strings.Cut(rest, "//")
		pkg, rule := splitPackageRule(pkgRule)
		return LabelParts{
			Repository: repo,
			Package:    pkg,
			RuleName:   rule,
			BuildFile:  buildFile(repo, pkg),
		}
	}
	if strings.HasPrefix(label, "@") {
		rest := strings.TrimPrefix(label, "@")
		repo, pkgRule, _ := strings.Cut(rest, "//")
		pkg, rule := splitPackageRule(pkgRule)
		return LabelParts{
			Repository: repo,
			Package:    pkg,
			RuleName:   rule,
			BuildFile:  buildFile(repo, pkg),
		}
	}
	if strings.HasPrefix(label, "//") {
		pkg, rule := splitPackageRule(strings.TrimPrefix(label, "//"))
		return LabelParts{
			Package:   pkg,
			RuleName:  rule,
			BuildFile: buildFile("", pkg),
		}
	}
	if !strings.Contains(label, ":") && !strings.Contains(label, "//") {
		return PackageParts(label)
	}
	return LabelParts{}
}

func PackageParts(pkg string) LabelParts {
	pkg = strings.Trim(strings.TrimSpace(pkg), "/")
	if pkg == "" {
		return LabelParts{BuildFile: "BUILD.bazel"}
	}
	if strings.HasPrefix(pkg, "@@") {
		rest := strings.TrimPrefix(pkg, "@@")
		repo, packageName, ok := strings.Cut(rest, "//")
		if !ok {
			return LabelParts{Repository: repo, BuildFile: buildFile(repo, "")}
		}
		packageName = strings.Trim(packageName, "/")
		return LabelParts{Repository: repo, Package: packageName, BuildFile: buildFile(repo, packageName)}
	}
	if strings.HasPrefix(pkg, "@") {
		rest := strings.TrimPrefix(pkg, "@")
		repo, packageName, ok := strings.Cut(rest, "//")
		if !ok {
			return LabelParts{Repository: repo, BuildFile: buildFile(repo, "")}
		}
		packageName = strings.Trim(packageName, "/")
		return LabelParts{Repository: repo, Package: packageName, BuildFile: buildFile(repo, packageName)}
	}
	return LabelParts{Package: pkg, BuildFile: buildFile("", pkg)}
}

func splitPackageRule(value string) (string, string) {
	value = strings.Trim(value, "/")
	pkg, rule, ok := strings.Cut(value, ":")
	if !ok {
		if value == "" {
			return "", ""
		}
		parts := strings.Split(value, "/")
		return value, parts[len(parts)-1]
	}
	return strings.Trim(pkg, "/"), strings.TrimSpace(rule)
}

func buildFile(repo, pkg string) string {
	pkg = strings.Trim(pkg, "/")
	if repo != "" {
		if pkg == "" {
			return "@@" + repo + "//BUILD.bazel"
		}
		return "@@" + repo + "//" + pkg + "/BUILD.bazel"
	}
	if pkg == "" {
		return "BUILD.bazel"
	}
	return pkg + "/BUILD.bazel"
}
