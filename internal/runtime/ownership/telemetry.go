package ownership

import (
	"fmt"
	"io"
)

func (event Event) String() string {
	switch event.Kind {
	case RegionCreated:
		return fmt.Sprintf("[ownership] region:%d created owner=%s", event.Region, event.Owner)
	case ObjectCreated:
		return fmt.Sprintf("[ownership] create #%d owner=%s region=%d refs=%d", event.Object, event.Owner, event.Region, event.References)
	case ObjectPublished:
		return fmt.Sprintf("[ownership] publish #%d %s -> %s region=%d refs=%d", event.Object, event.From, event.To, event.Region, event.References)
	case ObjectTransferred:
		return fmt.Sprintf("[ownership] transfer #%d %s -> %s region=%d refs=%d", event.Object, event.From, event.To, event.Region, event.References)
	case ObjectReleased:
		return fmt.Sprintf("[ownership] release #%d owner=%s refs=%d", event.Object, event.Owner, event.References)
	case ObjectDestroyed:
		return fmt.Sprintf("[ownership] object #%d destroyed", event.Object)
	case RegionReleased:
		return fmt.Sprintf("[ownership] region:%d released owner=%s", event.Region, event.Owner)
	case ObjectLinked:
		return fmt.Sprintf("[ownership] link #%d -> #%d", event.Object, event.Target)
	case ObjectUnlinked:
		return fmt.Sprintf("[ownership] unlink #%d -> #%d", event.Object, event.Target)
	case ObjectBarrierRetained:
		return fmt.Sprintf("[ownership] barrier retain #%d via #%d owner=%s region=%d refs=%d", event.Object, event.Target, event.Owner, event.Region, event.References)
	default:
		return fmt.Sprintf("[ownership] event:%d", event.Kind)
	}
}

// WriteTrace emits the ledger's deterministic event sequence.
func (ledger *Ledger) WriteTrace(writer io.Writer) error {
	if writer == nil {
		return fmt.Errorf("ownership: nil trace writer")
	}
	for _, event := range ledger.Events() {
		if _, err := fmt.Fprintln(writer, event.String()); err != nil {
			return fmt.Errorf("ownership: write trace: %w", err)
		}
	}
	return nil
}

// WriteSummary emits the current counters used by the Phase 0 experiment.
func (ledger *Ledger) WriteSummary(writer io.Writer) error {
	if writer == nil {
		return fmt.Errorf("ownership: nil summary writer")
	}
	stats := ledger.Stats()
	lines := []struct {
		label string
		value int
	}{
		{label: "Task-local allocations", value: stats.TaskLocalAllocations},
		{label: "Bulk-region releases", value: stats.BulkRegionReleases},
		{label: "Publish operations", value: stats.PublishOperations},
		{label: "Ownership transfers", value: stats.TransferOperations},
		{label: "Retain operations", value: stats.RetainOperations},
		{label: "Release operations", value: stats.ReleaseOperations},
		{label: "Local references", value: stats.LocalReferences},
		{label: "Barrier retains", value: stats.BarrierRetains},
		{label: "Persistent objects", value: stats.PersistentObjects},
		{label: "Live objects", value: stats.LiveObjects},
	}
	if _, err := fmt.Fprintln(writer, "Gossamer Memory"); err != nil {
		return fmt.Errorf("ownership: write summary: %w", err)
	}
	for _, line := range lines {
		if _, err := fmt.Fprintf(writer, "%-28s %d\n", line.label, line.value); err != nil {
			return fmt.Errorf("ownership: write summary: %w", err)
		}
	}
	return nil
}
