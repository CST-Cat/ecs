package ui

import (
	"bytes"
	"sync"
	"testing"

	"ecs/internal/model"
	"ecs/internal/runner"
	"ecs/internal/termcolor"
)

func TestProgressViewCompletesSafelyWithConcurrentUpdates(t *testing.T) {
	var output bytes.Buffer
	view := NewWithColor(&output, termcolor.LevelNone).BeginProgress(1)

	var group sync.WaitGroup
	for range 2 {
		group.Add(1)
		go func() {
			defer group.Done()
			view.Update(runner.Progress{
				Phase: runner.PhaseDone, Index: 1, Total: 1,
				Result: model.Result{Status: model.StatusOK},
			})
		}()
	}
	group.Wait()
	view.Stop()

	if view.doneCount != 1 {
		t.Fatalf("completed modules = %d, want one", view.doneCount)
	}
	if output.Len() != 0 {
		t.Fatalf("successful non-TTY progress should stay silent: %q", output.String())
	}
}
