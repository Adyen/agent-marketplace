package repair

import "strconv"

type JSONReport struct {
	Steps   []JSONStep       `json:"steps"`
	Results []JSONStepResult `json:"results"`

	ids        map[string]int
	indexes    map[string]int
	identities map[int]string
	hasResults map[int]bool
}

type JSONStep struct {
	ID        int         `json:"id"`
	Code      string      `json:"code"`
	Class     SafetyClass `json:"class"`
	Action    string      `json:"action"`
	Files     []string    `json:"files"`
	Status    string      `json:"status"`
	BlockedBy *int        `json:"blocked_by"`
}

type JSONStepResult struct {
	ID     int    `json:"id"`
	Status string `json:"status"`
}

func NewJSONReport() *JSONReport {
	return &JSONReport{
		Steps:      []JSONStep{},
		Results:    []JSONStepResult{},
		ids:        make(map[string]int),
		indexes:    make(map[string]int),
		identities: make(map[int]string),
		hasResults: make(map[int]bool),
	}
}

func (report *JSONReport) Display(steps []Step) error {
	keys := reportKeys(steps)
	ids := make([]int, len(steps))
	blockers := make(map[string]int, len(steps))
	active := make(map[string]bool, len(steps))
	for i, step := range steps {
		ids[i] = report.idFor(keys[i])
		active[keys[i]] = true
		if step.Key != "" {
			blockers[step.Key] = ids[i]
		}
	}
	report.retain(active)
	for i, step := range steps {
		report.ensureProjected(keys[i], step, ids[i])
		projected := &report.Steps[report.indexes[keys[i]]-1]
		projected.Code = step.Code
		projected.Class = step.Class
		projected.Action = step.Action
		projected.Files = append([]string{}, step.Files...)
		projected.BlockedBy = nil
		if step.BlockedBy != nil {
			if id, ok := blockers[step.BlockedBy.Key]; ok {
				blockedBy := id
				projected.BlockedBy = &blockedBy
			}
		}
		if !report.hasResults[ids[i]] {
			projected.Status = jsonStepStatus(step)
		}
	}
	return nil
}

func (report *JSONReport) RecordResult(step Step, applied bool, err error) {
	key := reportKey(identity(step), 0)
	id := report.idFor(key)
	report.ensureProjected(key, step, id)
	status := "completed"
	if err != nil {
		status = "failed"
	} else if !applied {
		status = "deferred"
	}
	report.Steps[report.indexes[key]-1].Status = status
	report.Results = append(report.Results, JSONStepResult{ID: id, Status: status})
	report.hasResults[id] = true
}

func reportKeys(steps []Step) []string {
	keys := make([]string, len(steps))
	occurrences := make(map[string]int, len(steps))
	for i, step := range steps {
		id := identity(step)
		keys[i] = reportKey(id, occurrences[id])
		occurrences[id]++
	}
	return keys
}

func reportKey(base string, occurrence int) string {
	return base + "\x00" + strconv.Itoa(occurrence)
}

func (report *JSONReport) idFor(key string) int {
	if id, ok := report.ids[key]; ok {
		return id
	}
	id := len(report.ids) + 1
	report.ids[key] = id
	report.identities[id] = key
	return id
}

func (report *JSONReport) ensureProjected(key string, step Step, id int) {
	if _, ok := report.indexes[key]; ok {
		return
	}
	report.indexes[key] = len(report.Steps) + 1
	report.Steps = append(report.Steps, JSONStep{
		ID:     id,
		Code:   step.Code,
		Class:  step.Class,
		Action: step.Action,
		Files:  append([]string{}, step.Files...),
		Status: jsonStepStatus(step),
	})
}

func (report *JSONReport) retain(active map[string]bool) {
	steps := make([]JSONStep, 0, len(report.Steps))
	indexes := make(map[string]int, len(report.indexes))
	for _, step := range report.Steps {
		key := report.identities[step.ID]
		if !active[key] && !report.hasResults[step.ID] {
			continue
		}
		steps = append(steps, step)
		indexes[key] = len(steps)
	}
	report.Steps = steps
	report.indexes = indexes
}

func jsonStepStatus(step Step) string {
	if step.BlockedBy != nil || step.Class == HumanOnly {
		return "blocked"
	}
	if step.Class == Destructive || step.Class == Guided {
		return "deferred"
	}
	return "pending"
}
