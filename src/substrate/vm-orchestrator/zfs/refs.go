package zfs

import (
	"fmt"
	"path"
	"regexp"
	"strings"
)

const (
	readySnapshot  = "ready"
	sealedSnapshot = "sealed"
)

var (
	refPattern      = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)
	snapshotPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)
)

type Roots struct {
	Pool            string
	ImageDataset    string
	OrgDataset      string
	GoldenDataset   string
	WorkloadDataset string
}

func (r Roots) normalized() Roots {
	out := Roots{
		Pool:            cleanSegment(firstNonEmpty(r.Pool, "vspool")),
		ImageDataset:    cleanSegment(firstNonEmpty(r.ImageDataset, "images")),
		OrgDataset:      cleanSegment(firstNonEmpty(r.OrgDataset, "orgs")),
		GoldenDataset:   cleanSegment(firstNonEmpty(r.GoldenDataset, "goldens")),
		WorkloadDataset: cleanSegment(firstNonEmpty(r.WorkloadDataset, "workloads")),
	}
	return out
}

func (r Roots) imageRoot() string {
	r = r.normalized()
	return path.Join(r.Pool, r.ImageDataset)
}

func (r Roots) orgsRoot() string {
	r = r.normalized()
	return path.Join(r.Pool, r.OrgDataset)
}

func (r Roots) OrgsRootDataset() string { return r.orgsRoot() }

func (r Roots) orgRoot(orgID string) string {
	r = r.normalized()
	return path.Join(r.orgsRoot(), orgID)
}

func (r Roots) OrgRootDataset(orgID string) string { return r.orgRoot(orgID) }

func (r Roots) workloadRoot(orgID string) string {
	r = r.normalized()
	return path.Join(r.orgRoot(orgID), r.WorkloadDataset)
}

func (r Roots) orgImageRoot(orgID string) string {
	r = r.normalized()
	return path.Join(r.orgRoot(orgID), "images")
}

func (r Roots) goldenRoot(orgID string) string {
	r = r.normalized()
	return path.Join(r.orgRoot(orgID), r.GoldenDataset)
}

type Snapshot struct {
	dataset string
	name    string
}

func (s Snapshot) Dataset() string { return s.dataset }
func (s Snapshot) Name() string    { return s.name }
func (s Snapshot) String() string {
	if s.dataset == "" || s.name == "" {
		return ""
	}
	return s.dataset + "@" + s.name
}

type Image struct {
	roots Roots
	ref   string
}

func NewImage(roots Roots, ref string) (Image, error) {
	ref = strings.TrimSpace(ref)
	if !IsValidRef(ref) {
		return Image{}, fmt.Errorf("image ref is invalid: %s", ref)
	}
	return Image{roots: roots.normalized(), ref: ref}, nil
}

func (i Image) Ref() string        { return i.ref }
func (i Image) Dataset() string    { return path.Join(i.roots.imageRoot(), i.ref) }
func (i Image) Snapshot() Snapshot { return Snapshot{dataset: i.Dataset(), name: readySnapshot} }

type Lease struct {
	roots Roots
	orgID string
	id    string
}

func NewLease(roots Roots, orgID, leaseID string) (Lease, error) {
	orgID = strings.TrimSpace(orgID)
	if !IsValidRef(orgID) {
		return Lease{}, fmt.Errorf("lease org id is invalid: %s", orgID)
	}
	leaseID = strings.TrimSpace(leaseID)
	if !IsValidRef(leaseID) {
		return Lease{}, fmt.Errorf("lease id is invalid: %s", leaseID)
	}
	return Lease{roots: roots.normalized(), orgID: orgID, id: leaseID}, nil
}

func (l Lease) OrgID() string       { return l.orgID }
func (l Lease) ID() string          { return l.id }
func (l Lease) Dataset() string     { return path.Join(l.roots.workloadRoot(l.orgID), l.id) }
func (l Lease) RootDataset() string { return path.Join(l.Dataset(), "root") }
func (l Lease) MountDataset(index int, name string) (string, error) {
	if index < 0 {
		return "", fmt.Errorf("mount index is invalid: %d", index)
	}
	name = strings.TrimSpace(name)
	if !IsValidRef(name) {
		return "", fmt.Errorf("mount name is invalid: %s", name)
	}
	return path.Join(l.Dataset(), "mounts", fmt.Sprintf("%02d-%s", index, name)), nil
}

type Volume struct {
	roots Roots
	orgID string
	id    string
}

func NewVolume(roots Roots, orgID, volumeID string) (Volume, error) {
	orgID = strings.TrimSpace(orgID)
	if !IsValidRef(orgID) {
		return Volume{}, fmt.Errorf("volume org id is invalid: %s", orgID)
	}
	volumeID = strings.TrimSpace(volumeID)
	if !IsValidRef(volumeID) {
		return Volume{}, fmt.Errorf("volume id is invalid: %s", volumeID)
	}
	return Volume{roots: roots.normalized(), orgID: orgID, id: volumeID}, nil
}

func (v Volume) OrgID() string   { return v.orgID }
func (v Volume) ID() string      { return v.id }
func (v Volume) Dataset() string { return path.Join(v.roots.goldenRoot(v.orgID), v.id) }
func (v Volume) GenerationDataset(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("generation name is required")
	}
	if err := ValidateSnapshotName(name); err != nil {
		return "", err
	}
	return path.Join(v.Dataset(), "generations", name), nil
}

type Generation struct {
	volume Volume
	snap   Snapshot
}

func ParseGeneration(roots Roots, ref string) (Generation, error) {
	ref = strings.TrimSpace(ref)
	dataset, snapName, ok := strings.Cut(ref, "@")
	if !ok || dataset == "" || snapName == "" {
		return Generation{}, fmt.Errorf("generation ref must be <dataset>@<snapshot>")
	}
	if err := ValidateSnapshotName(snapName); err != nil {
		return Generation{}, err
	}
	roots = roots.normalized()
	prefix := roots.orgsRoot() + "/"
	if !strings.HasPrefix(dataset, prefix) {
		return Generation{}, fmt.Errorf("generation dataset is outside org root")
	}
	relative := strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(dataset, prefix), "/"), "/")
	parts := strings.Split(relative, "/")
	if len(parts) != 5 || parts[1] != roots.GoldenDataset || parts[3] != "generations" {
		return Generation{}, fmt.Errorf("generation dataset is outside org golden root")
	}
	orgID := parts[0]
	volumeID := parts[2]
	if volumeID == "" {
		return Generation{}, fmt.Errorf("generation volume id is empty")
	}
	if !IsValidRef(orgID) {
		return Generation{}, fmt.Errorf("generation org id is invalid: %s", orgID)
	}
	if !IsValidRef(volumeID) {
		return Generation{}, fmt.Errorf("generation volume id is invalid: %s", volumeID)
	}
	volume, err := NewVolume(roots, orgID, volumeID)
	if err != nil {
		return Generation{}, err
	}
	return Generation{volume: volume, snap: Snapshot{dataset: dataset, name: snapName}}, nil
}

func (g Generation) Volume() Volume     { return g.volume }
func (g Generation) Snapshot() Snapshot { return g.snap }
func (g Generation) String() string     { return g.snap.String() }

type MountClone struct {
	lease   Lease
	dataset string
	name    string
}

func (m MountClone) Lease() Lease       { return m.lease }
func (m MountClone) Dataset() string    { return m.dataset }
func (m MountClone) Name() string       { return m.name }
func (m MountClone) Snapshot() Snapshot { return Snapshot{dataset: m.dataset, name: sealedSnapshot} }

func IsValidRef(ref string) bool {
	ref = strings.TrimSpace(ref)
	return refPattern.MatchString(ref) && !strings.Contains(ref, "/") && !strings.Contains(ref, "@") && !strings.Contains(ref, "..")
}

func ValidateSnapshotName(name string) error {
	name = strings.TrimSpace(name)
	if !snapshotPattern.MatchString(name) || strings.Contains(name, "/") || strings.Contains(name, "@") || strings.Contains(name, "..") {
		return fmt.Errorf("snapshot name is invalid: %s", name)
	}
	return nil
}

func cleanSegment(value string) string {
	value = strings.Trim(value, "/ ")
	if value == "" {
		return ""
	}
	parts := strings.Split(value, "/")
	for i, part := range parts {
		parts[i] = strings.TrimSpace(part)
	}
	return path.Join(parts...)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
