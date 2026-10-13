package labels

import (
	"sort"
	"strings"
)

// Problem is one validation failure. Path names the offending property, such
// as "elements[2].font_size"; it is empty for document-level problems.
type Problem struct {
	Path    string `json:"path,omitempty"`
	Message string `json:"message"`
}

// ValidationError collects every problem found in a template.
type ValidationError struct {
	Problems []Problem
}

func (e *ValidationError) Error() string {
	parts := make([]string, len(e.Problems))
	for i, p := range e.Problems {
		if p.Path == "" {
			parts[i] = p.Message
		} else {
			parts[i] = p.Path + ": " + p.Message
		}
	}
	return "invalid template: " + strings.Join(parts, "; ")
}

type validator struct {
	problems []Problem
}

func (v *validator) add(path, message string) {
	v.problems = append(v.problems, Problem{Path: path, Message: message})
}

func (v *validator) err() error {
	if len(v.problems) == 0 {
		return nil
	}
	// Map iteration makes some checks unordered; sort for stable output.
	sort.SliceStable(v.problems, func(i, j int) bool { return v.problems[i].Path < v.problems[j].Path })
	return &ValidationError{Problems: v.problems}
}
