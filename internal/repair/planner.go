package repair

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"sort"
	"strings"
)

type Stage int

const (
	StageJournal Stage = iota + 1
	StageConfiguration
	StageLocks
	StageCache
	StageDrift
	StageOwnership
)

type SafetyClass string

const (
	Safe        SafetyClass = "safe"
	Destructive SafetyClass = "destructive"
	Guided      SafetyClass = "guided"
	HumanOnly   SafetyClass = "human-only"
)

type Input string

const InputSelection Input = "selection"

type Blocker struct {
	Key    string
	Reason string
}

type Step struct {
	Key       string
	Stage     Stage
	Class     SafetyClass
	Action    string
	Code      string
	Files     []string
	Inputs    []Input
	BlockedBy *Blocker
	Apply     func(context.Context) (bool, error)
}

type Diagnosis struct {
	Steps         []Step
	InvalidInputs map[Input]Blocker
}

func Plan(diagnosis Diagnosis) []Step {
	steps := make([]Step, len(diagnosis.Steps))
	for i, step := range diagnosis.Steps {
		steps[i] = step
		steps[i].Files = append([]string(nil), step.Files...)
		steps[i].Inputs = append([]Input(nil), step.Inputs...)
		if step.BlockedBy == nil {
			for _, input := range step.Inputs {
				if blocker, invalid := diagnosis.InvalidInputs[input]; invalid {
					blockerCopy := blocker
					steps[i].BlockedBy = &blockerCopy
					break
				}
			}
		}
	}
	sort.SliceStable(steps, func(i, j int) bool {
		if steps[i].Stage != steps[j].Stage {
			return steps[i].Stage < steps[j].Stage
		}
		if steps[i].Code != steps[j].Code {
			return steps[i].Code < steps[j].Code
		}
		return steps[i].Action < steps[j].Action
	})
	return steps
}

func WritePlan(out io.Writer, steps []Step) error {
	if _, err := fmt.Fprintln(out, "plan"); err != nil {
		return err
	}
	for i, step := range steps {
		files := strings.Join(step.Files, ", ")
		if files == "" {
			files = "none"
		}
		detail := "needs: none"
		if step.BlockedBy != nil {
			blocker := 0
			for j, candidate := range steps {
				if candidate.Key != "" && candidate.Key == step.BlockedBy.Key {
					blocker = j + 1
					break
				}
			}
			if blocker == 0 {
				return fmt.Errorf("repair plan blocker %q has no listed item", step.BlockedBy.Key)
			}
			detail = fmt.Sprintf("blocked by %d (%s)", blocker, step.BlockedBy.Reason)
		} else if len(step.Inputs) > 0 {
			inputs := make([]string, len(step.Inputs))
			for j, input := range step.Inputs {
				inputs[j] = string(input)
			}
			detail = "needs: " + strings.Join(inputs, ", ")
		}
		if _, err := fmt.Fprintf(out, "  %d. [%s] %s (%s; files: %s; %s)\n",
			i+1, step.Class, step.Action, step.Code, files, detail); err != nil {
			return err
		}
	}
	return nil
}

type Loop struct {
	Diagnose         func(context.Context) (Diagnosis, error)
	Confirm          func([]Step) bool
	Validate         func() error
	Display          func([]Step) error
	AllowDestructive bool
	AllowGuided      bool
}

var ErrIncomplete = errors.New("repair remains blocked")

func HasBlockedSteps(steps []Step) bool {
	return slices.ContainsFunc(steps, func(step Step) bool {
		return step.BlockedBy != nil || step.Class == HumanOnly || step.Class == Guided
	})
}

func (loop Loop) Run(ctx context.Context) error {
	confirmed := make(map[string]bool)
	deferred := make(map[string]bool)
	runs := make(map[string]int)
	for {
		diagnosis, err := loop.Diagnose(ctx)
		if err != nil {
			return err
		}
		steps := Plan(diagnosis)
		if loop.Display != nil {
			if err := loop.Display(steps); err != nil {
				return err
			}
		}
		for _, step := range steps {
			if runs[identity(step)] >= 5 {
				return fmt.Errorf("repair step %q kept recurring after 5 runs", step.Action)
			}
		}
		var safe []Step
		for _, step := range steps {
			if step.BlockedBy == nil && step.Class == Safe && !confirmed[identity(step)] {
				safe = append(safe, step)
			}
		}
		if len(safe) > 0 {
			if loop.Confirm == nil || !loop.Confirm(safe) {
				return nil
			}
			for _, step := range safe {
				confirmed[identity(step)] = true
			}
		}
		var next *Step
		for i := range steps {
			step := &steps[i]
			if step.BlockedBy != nil || step.Class == HumanOnly || step.Class == Guided && !loop.AllowGuided {
				continue
			}
			key := identity(*step)
			if deferred[key] || step.Class == Safe && !confirmed[key] || step.Class == Destructive && !loop.AllowDestructive {
				continue
			}
			next = step
			break
		}
		if next == nil {
			if HasBlockedSteps(steps) {
				return ErrIncomplete
			}
			return nil
		}
		if next.Apply == nil {
			return fmt.Errorf("repair step %q has no implementation", next.Action)
		}
		if loop.Validate != nil {
			if err := loop.Validate(); err != nil {
				return err
			}
		}
		applied, err := next.Apply(ctx)
		if err != nil {
			return err
		}
		if next.Class == Destructive || next.Class == Guided {
			deferred[identity(*next)] = true
		}
		if applied {
			runs[identity(*next)]++
		} else {
			deferred[identity(*next)] = true
		}
	}
}

func identity(step Step) string {
	files := append([]string(nil), step.Files...)
	sort.Strings(files)
	return fmt.Sprintf("%d\x00%s\x00%s\x00%s", step.Stage, step.Action, step.Code, strings.Join(files, "\x00"))
}
