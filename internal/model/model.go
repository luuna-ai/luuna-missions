package model

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

const (
	MissionsDir         = ".luuna/missions"
	IssuesFile          = "issues.jsonl"
	PlansFile           = "plans.jsonl"
	TemplatesFile       = "templates.jsonl"
	ConfigFile          = "config.yaml"
	DefaultMaxPlanDepth = 3
)

var (
	ValidTypes     = []string{"task", "bug", "feature", "epic"}
	ValidStatuses  = []string{"open", "in_progress", "closed"}
	PriorityLabels = map[int]string{
		0: "Critical", 1: "High", 2: "Medium", 3: "Low", 4: "Backlog",
	}
)

// Issue mirrors the JSONL record contract in the public record schema. The pointer for
// PlanStepIndex distinguishes an absent field from the valid zero index.
type Issue struct {
	ID             string         `json:"id"`
	Title          string         `json:"title"`
	Status         string         `json:"status"`
	Type           string         `json:"type"`
	Priority       int            `json:"priority"`
	CreatedAt      string         `json:"createdAt"`
	UpdatedAt      string         `json:"updatedAt"`
	Assignee       string         `json:"assignee,omitempty"`
	Description    string         `json:"description,omitempty"`
	CloseReason    string         `json:"closeReason,omitempty"`
	Blocks         []string       `json:"blocks,omitempty"`
	BlockedBy      []string       `json:"blockedBy,omitempty"`
	Labels         []string       `json:"labels,omitempty"`
	Convoy         string         `json:"convoy,omitempty"`
	PlanID         string         `json:"plan_id,omitempty"`
	PlanStepIndex  *int           `json:"plan_step_index,omitempty"`
	RequiresPlan   bool           `json:"requires_plan,omitempty"`
	DiscoveredFrom string         `json:"discoveredFrom,omitempty"`
	Intent         any            `json:"intent,omitempty"`
	Extensions     map[string]any `json:"extensions,omitempty"`
	ClosedAt       string         `json:"closedAt,omitempty"`
	state          recordState
}

func (issue Issue) GetID() string { return issue.ID }

type Plan struct {
	ID              string         `json:"id"`
	Seed            string         `json:"seed"`
	Template        string         `json:"template"`
	Name            string         `json:"name,omitempty"`
	Status          string         `json:"status"`
	Revision        int            `json:"revision"`
	Sections        map[string]any `json:"sections"`
	Children        []string       `json:"children"`
	AdoptedChildren []string       `json:"adoptedChildren,omitempty"`
	Outcome         string         `json:"outcome,omitempty"`
	OutcomeNote     string         `json:"outcomeNote,omitempty"`
	ReviewedBy      string         `json:"reviewedBy,omitempty"`
	CreatedAt       string         `json:"createdAt"`
	UpdatedAt       string         `json:"updatedAt"`
	state           recordState
	sectionOrder    []string
}

func (plan Plan) GetID() string { return plan.ID }

type TemplateStep struct {
	Title        string `json:"title"`
	Type         string `json:"type,omitempty"`
	Priority     *int   `json:"priority,omitempty"`
	PlanTemplate string `json:"plan_template,omitempty"`
	state        recordState
}

type Template struct {
	ID    string         `json:"id"`
	Name  string         `json:"name"`
	Steps []TemplateStep `json:"steps"`
	state recordState
}

func (template Template) GetID() string { return template.ID }

type Config struct {
	Project      string `json:"project" yaml:"project"`
	Version      string `json:"version" yaml:"version"`
	MaxPlanDepth *int   `json:"max_plan_depth,omitempty" yaml:"max_plan_depth,omitempty"`
}

// SectionSpec's Kind and Item deliberately admit either a scalar or a nested
// mapping. Plan template validation lives at the config/plan boundary.
type SectionSpec struct {
	Required    bool   `json:"required" yaml:"required"`
	Kind        any    `json:"kind" yaml:"kind"`
	Prompt      string `json:"prompt" yaml:"prompt"`
	MinLength   *int   `json:"min_length,omitempty" yaml:"min_length,omitempty"`
	Min         *int   `json:"min,omitempty" yaml:"min,omitempty"`
	Item        any    `json:"item,omitempty" yaml:"item,omitempty"`
	AtlasSource string `json:"atlas_source,omitempty" yaml:"atlas_source,omitempty"`
}

type PlanTemplate struct {
	Name         string                 `json:"name" yaml:"-"`
	Description  string                 `json:"description,omitempty" yaml:"description,omitempty"`
	Sections     map[string]SectionSpec `json:"sections" yaml:"sections"`
	SectionOrder []string               `json:"-" yaml:"-"`
}

func IntPtr(v int) *int { return &v }

// NowISO matches the public timestamp contract: UTC with exactly three decimal
// digits, including trailing zeros.
func NowISO() string { return time.Now().UTC().Format("2006-01-02T15:04:05.000Z") }

// GenerateID uses the same project-prefix and four-hex scheme as the original CLI.
// After 100 collisions it falls back to eight hex digits.
func GenerateID(prefix string, existing map[string]bool) (string, error) {
	makeID := func(hexDigits int) (string, error) {
		buf := make([]byte, (hexDigits+1)/2)
		if _, err := rand.Read(buf); err != nil {
			return "", err
		}
		return prefix + "-" + hex.EncodeToString(buf)[:hexDigits], nil
	}
	for i := 0; i < 100; i++ {
		id, err := makeID(4)
		if err != nil {
			return "", err
		}
		if !existing[id] {
			return id, nil
		}
	}
	for {
		id, err := makeID(8)
		if err != nil {
			return "", fmt.Errorf("generate id: %w", err)
		}
		if !existing[id] {
			return id, nil
		}
	}
}
