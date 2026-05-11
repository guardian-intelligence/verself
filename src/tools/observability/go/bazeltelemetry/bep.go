package bazeltelemetry

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
)

func ParseBEP(path string) ([]Target, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open bep: %w", err)
	}
	defer func() { _ = f.Close() }()

	namedSets := make(map[string]rawBEPNamedSetOfFiles)
	pending := make([]targetWithFileSets, 0, 1024)
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var raw rawBEPEvent
		if err := json.Unmarshal(line, &raw); err != nil {
			return nil, fmt.Errorf("decode bep line: %w", err)
		}
		switch {
		case raw.ID.NamedSet != nil && raw.NamedSetOfFiles != nil:
			namedSets[raw.ID.NamedSet.ID] = *raw.NamedSetOfFiles
		case raw.ID.TargetCompleted != nil && raw.Completed != nil:
			parts := ParseLabel(raw.ID.TargetCompleted.Label)
			target := Target{
				Label:            raw.ID.TargetCompleted.Label,
				Package:          parts.Package,
				BuildFile:        parts.BuildFile,
				RuleName:         parts.RuleName,
				Success:          raw.Completed.Success,
				OutputGroupCount: len(raw.Completed.OutputGroups),
			}
			var fileSets []string
			for _, group := range raw.Completed.OutputGroups {
				for _, fileSet := range group.FileSets {
					fileSets = append(fileSets, fileSet.ID)
				}
			}
			pending = append(pending, targetWithFileSets{Target: target, FileSets: fileSets})
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read bep: %w", err)
	}
	out := make([]Target, 0, len(pending))
	for _, item := range pending {
		seen := make(map[string]struct{})
		for _, fileSet := range item.FileSets {
			item.OutputFileCount += countBEPFiles(namedSets, fileSet, seen)
		}
		out = append(out, item.Target)
	}
	return out, nil
}

type rawBEPEvent struct {
	ID              rawBEPEventID          `json:"id"`
	Completed       *rawBEPCompleted       `json:"completed,omitempty"`
	NamedSetOfFiles *rawBEPNamedSetOfFiles `json:"namedSetOfFiles,omitempty"`
}

type rawBEPEventID struct {
	TargetCompleted *rawBEPTargetCompletedID `json:"targetCompleted,omitempty"`
	NamedSet        *rawBEPNamedSetID        `json:"namedSet,omitempty"`
}

type rawBEPTargetCompletedID struct {
	Label string `json:"label"`
}

type rawBEPCompleted struct {
	Success      bool                `json:"success"`
	OutputGroups []rawBEPOutputGroup `json:"outputGroup"`
}

type rawBEPOutputGroup struct {
	Name     string             `json:"name"`
	FileSets []rawBEPNamedSetID `json:"fileSets"`
}

type rawBEPNamedSetID struct {
	ID string `json:"id"`
}

type rawBEPNamedSetOfFiles struct {
	Files    []rawBEPFile       `json:"files"`
	FileSets []rawBEPNamedSetID `json:"fileSets"`
}

type rawBEPFile struct {
	Name string `json:"name"`
	URI  string `json:"uri"`
}

type targetWithFileSets struct {
	Target
	FileSets []string
}

func countBEPFiles(namedSets map[string]rawBEPNamedSetOfFiles, setID string, seen map[string]struct{}) int {
	if _, ok := seen[setID]; ok {
		return 0
	}
	seen[setID] = struct{}{}
	set, ok := namedSets[setID]
	if !ok {
		return 0
	}
	count := len(set.Files)
	for _, child := range set.FileSets {
		count += countBEPFiles(namedSets, child.ID, seen)
	}
	return count
}
