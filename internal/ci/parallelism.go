package ci

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"
)

type leaseWitness struct {
	leaseDir  string
	startedAt time.Time
	stopCh    chan struct{}
	doneCh    chan leaseWitnessSummary
}

var sysClassNetPath = "/sys/class/net"

type leaseWitnessSummary struct {
	MaxActiveLeases int
	FirstOverlapAt  time.Time
	DistinctJobIDs  []string
	Samples         int
	ReadErrors      int
}

type leaseFile struct {
	JobID        string    `json:"job_id"`
	PID          int       `json:"pid,omitempty"`
	TapName      string    `json:"tap_name,omitempty"`
	CreatedAtUTC time.Time `json:"created_at_utc"`
}

func startLeaseWitness(leaseDir string) *leaseWitness {
	w := &leaseWitness{
		leaseDir:  leaseDir,
		startedAt: time.Now().UTC(),
		stopCh:    make(chan struct{}),
		doneCh:    make(chan leaseWitnessSummary, 1),
	}
	go w.run()
	return w
}

func (w *leaseWitness) Stop() leaseWitnessSummary {
	close(w.stopCh)
	return <-w.doneCh
}

func (w *leaseWitness) run() {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	summary := leaseWitnessSummary{}
	distinct := map[string]struct{}{}
	for {
		select {
		case <-w.stopCh:
			summary.DistinctJobIDs = sortedKeys(distinct)
			w.doneCh <- summary
			return
		case <-ticker.C:
			active, jobIDs, err := sampleLeases(w.leaseDir, w.startedAt)
			summary.Samples++
			if err != nil {
				summary.ReadErrors++
				continue
			}
			if active > summary.MaxActiveLeases {
				summary.MaxActiveLeases = active
			}
			if active >= 2 && summary.FirstOverlapAt.IsZero() {
				summary.FirstOverlapAt = time.Now().UTC()
			}
			for _, jobID := range jobIDs {
				distinct[jobID] = struct{}{}
			}
		}
	}
}

func sampleLeases(leaseDir string, minCreatedAt time.Time) (int, []string, error) {
	entries, err := os.ReadDir(leaseDir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil, nil
		}
		return 0, nil, err
	}

	jobIDs := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(leaseDir, entry.Name()))
		if err != nil {
			return 0, nil, err
		}
		var lease leaseFile
		if err := json.Unmarshal(data, &lease); err != nil {
			return 0, nil, err
		}
		if lease.JobID == "" {
			continue
		}
		if !lease.CreatedAtUTC.IsZero() && lease.CreatedAtUTC.Before(minCreatedAt) {
			continue
		}
		// Count only live VMs. A lease can exist before the jailer starts and
		// picks up a PID, which is too early to treat as real overlap.
		if lease.PID <= 0 || !processExists(lease.PID) {
			continue
		}
		if lease.TapName != "" && !tapExists(lease.TapName) {
			continue
		}
		jobIDs = append(jobIDs, lease.JobID)
	}
	sort.Strings(jobIDs)
	return len(jobIDs), jobIDs, nil
}

func sortedKeys(values map[string]struct{}) []string {
	out := make([]string, 0, len(values))
	for value := range values {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func processExists(pid int) bool {
	if pid <= 0 {
		return false
	}
	_, err := os.Stat(filepath.Join("/proc", strconv.Itoa(pid)))
	return err == nil
}

func tapExists(name string) bool {
	if name == "" {
		return false
	}
	_, err := os.Stat(filepath.Join(sysClassNetPath, name))
	return err == nil
}
