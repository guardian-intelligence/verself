package zfsharness

import (
	"context"
	"fmt"
)

// RecoverOrphans finds CI clones that lack a @done snapshot and destroys
// them. Called on agent startup for crash recovery.
//
// Pattern: OBuilder's @snap — snapshot presence = completed build.
// "Clone without @done → orphaned build → destroy."
func (h *Harness) RecoverOrphans(ctx context.Context) ([]string, error) {
	ciDS := h.ciDatasetPath()

	children, err := h.exec.listChildren(ctx, ciDS)
	if err != nil {
		return nil, fmt.Errorf("list CI clones: %w", err)
	}

	var destroyed []string
	for _, child := range children {
		if child.Name == ciDS {
			continue
		}

		done, err := h.exec.exists(ctx, child.Name+"@done")
		if err != nil {
			continue
		}

		if !done {
			if err := h.exec.destroy(ctx, child.Name, true); err == nil {
				destroyed = append(destroyed, child.Name)
			}
		}
	}
	return destroyed, nil
}
