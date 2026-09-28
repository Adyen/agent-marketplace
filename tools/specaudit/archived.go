package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
)

var taskLine = regexp.MustCompile(`(?m)^\s*- \[([ xX])\]`)

func archived(root string) ([]finding, string, error) {
	changes, err := activeChanges(root)
	if err != nil {
		return nil, "", err
	}
	var findings []finding
	for _, change := range changes {
		data, err := os.ReadFile(filepath.Join(root, changesDir, change, "tasks.md"))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, "", err
		}
		tasks := taskLine.FindAllSubmatch(data, -1)
		open := 0
		for _, t := range tasks {
			if string(t[1]) == " " {
				open++
			}
		}
		if len(tasks) > 0 && open == 0 {
			findings = append(findings, finding{kindUnarchivedChange, fmt.Sprintf(
				"%s has all %d tasks checked; run openspec archive %s", change, len(tasks), change)})
		}
	}
	return findings, fmt.Sprintf("%d active changes", len(changes)), nil
}
