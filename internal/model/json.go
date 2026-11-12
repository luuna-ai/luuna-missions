package model

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
)

// recordState retains fields that the current public types do not describe,
// plus the original presence/value of known optional fields (including null).
// Go commands may update typed fields without discarding unrelated on-disk
// metadata owned by a future schema or another compatible writer.
type recordState struct {
	original map[string]json.RawMessage
	baseline map[string]json.RawMessage
	order    []string
}

func parseObject(data []byte) (map[string]json.RawMessage, []string, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	open, err := dec.Token()
	if err != nil {
		return nil, nil, err
	}
	if open != json.Delim('{') {
		return nil, nil, fmt.Errorf("record must be a JSON object")
	}
	fields := map[string]json.RawMessage{}
	order := []string{}
	for dec.More() {
		token, err := dec.Token()
		if err != nil {
			return nil, nil, err
		}
		key, ok := token.(string)
		if !ok {
			return nil, nil, fmt.Errorf("record has a non-string key")
		}
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return nil, nil, err
		}
		if _, seen := fields[key]; !seen {
			order = append(order, key)
		}
		fields[key] = append(json.RawMessage(nil), raw...)
	}
	closeToken, err := dec.Token()
	if err != nil {
		return nil, nil, err
	}
	if closeToken != json.Delim('}') {
		return nil, nil, fmt.Errorf("record is not terminated")
	}
	return fields, order, nil
}

func knownJSONFields(typeOf reflect.Type) map[string]bool {
	known := map[string]bool{}
	for i := 0; i < typeOf.NumField(); i++ {
		field := typeOf.Field(i)
		tag := field.Tag.Get("json")
		if tag == "" || tag == "-" {
			continue
		}
		key := strings.SplitN(tag, ",", 2)[0]
		if key != "" {
			known[key] = true
		}
	}
	return known
}

// JSONL readers in the original CLI parse JSON objects without validating
// every known field. Decode fields independently so one malformed optional
// value does not hide the whole record from commands or doctor diagnostics.
// The raw value remains in recordState until a command explicitly changes it.
func decodeKnownObject(data []byte, target any) error {
	fields, _, err := parseObject(data)
	if err != nil {
		return err
	}
	value := reflect.ValueOf(target).Elem()
	typeOf := value.Type()
	for i := 0; i < typeOf.NumField(); i++ {
		field := typeOf.Field(i)
		tag := field.Tag.Get("json")
		if tag == "" || tag == "-" {
			continue
		}
		key := strings.SplitN(tag, ",", 2)[0]
		raw, present := fields[key]
		if !present || !value.Field(i).CanAddr() {
			continue
		}
		_ = json.Unmarshal(raw, value.Field(i).Addr().Interface())
	}
	return nil
}

func (state *recordState) capture(data, decoded []byte) error {
	original, order, err := parseObject(data)
	if err != nil {
		return err
	}
	baseline, _, err := parseObject(decoded)
	if err != nil {
		return err
	}
	state.original, state.baseline, state.order = original, baseline, order
	return nil
}

func compactRaw(raw json.RawMessage) json.RawMessage {
	var out bytes.Buffer
	if err := json.Compact(&out, raw); err != nil {
		return raw
	}
	return out.Bytes()
}

func (state recordState) merge(current []byte, known map[string]bool) ([]byte, error) {
	if state.original == nil {
		return current, nil
	}
	values, currentOrder, err := parseObject(current)
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	out.WriteByte('{')
	written := map[string]bool{}
	appendField := func(key string, value json.RawMessage) error {
		if written[key] {
			return nil
		}
		if len(value) == 0 {
			return nil
		}
		if len(written) > 0 {
			out.WriteByte(',')
		}
		encodedKey, err := json.Marshal(key)
		if err != nil {
			return err
		}
		out.Write(encodedKey)
		out.WriteByte(':')
		out.Write(compactRaw(value))
		written[key] = true
		return nil
	}
	for _, key := range state.order {
		original := state.original[key]
		if !known[key] {
			if err := appendField(key, original); err != nil {
				return nil, err
			}
			continue
		}
		value, present := values[key]
		baseline, before := state.baseline[key]
		if (!present && !before) || (present && before && bytes.Equal(value, baseline)) {
			if err := appendField(key, original); err != nil {
				return nil, err
			}
			continue
		}
		if present {
			if err := appendField(key, value); err != nil {
				return nil, err
			}
		}
	}
	for _, key := range currentOrder {
		if err := appendField(key, values[key]); err != nil {
			return nil, err
		}
	}
	out.WriteByte('}')
	return out.Bytes(), nil
}

type issueWire Issue

func (issue *Issue) UnmarshalJSON(data []byte) error {
	var decoded issueWire
	if err := decodeKnownObject(data, &decoded); err != nil {
		return err
	}
	*issue = Issue(decoded)
	baseline, err := marshalIssueWire(issueWire(*issue))
	if err != nil {
		return err
	}
	return issue.state.capture(data, baseline)
}

func (issue Issue) MarshalJSON() ([]byte, error) {
	current, err := marshalIssueWire(issueWire(issue))
	if err != nil {
		return nil, err
	}
	return issue.state.merge(current, knownJSONFields(reflect.TypeOf(issueWire{})))
}

// encoding/json omitempty drops both nil and non-nil empty slices. The original encoder
// distinguishes an omitted property from an explicitly empty array, so retain
// that distinction for the three optional Issue arrays.
func marshalIssueWire(issue issueWire) ([]byte, error) {
	current, err := json.Marshal(issue)
	if err != nil {
		return nil, err
	}
	empty := []string{}
	if issue.Blocks != nil && len(issue.Blocks) == 0 {
		empty = append(empty, `"blocks":[]`)
	}
	if issue.BlockedBy != nil && len(issue.BlockedBy) == 0 {
		empty = append(empty, `"blockedBy":[]`)
	}
	if issue.Labels != nil && len(issue.Labels) == 0 {
		empty = append(empty, `"labels":[]`)
	}
	if len(empty) == 0 {
		return current, nil
	}
	if len(current) < 2 || current[len(current)-1] != '}' {
		return nil, fmt.Errorf("issue JSON not an object")
	}
	var out bytes.Buffer
	out.Write(current[:len(current)-1])
	if len(current) > 2 {
		out.WriteByte(',')
	}
	out.WriteString(strings.Join(empty, ","))
	out.WriteByte('}')
	return out.Bytes(), nil
}

// SectionKeys returns the persisted declaration order of a plan's sections.
// For a newly constructed plan with no source JSON, it returns sorted keys.
func (plan Plan) SectionKeys() []string {
	order := plan.sectionOrder
	if len(order) == 0 {
		if raw, ok := plan.state.original["sections"]; ok {
			_, order, _ = parseObject(raw)
		}
	}
	keys := make([]string, 0, len(plan.Sections))
	seen := map[string]bool{}
	for _, key := range order {
		if _, exists := plan.Sections[key]; exists && !seen[key] {
			keys, seen[key] = append(keys, key), true
		}
	}
	extra := []string{}
	for key := range plan.Sections {
		if !seen[key] {
			extra = append(extra, key)
		}
	}
	sort.Strings(extra)
	return append(keys, extra...)
}

func (plan *Plan) SetSectionOrder(keys []string) { plan.sectionOrder = append([]string(nil), keys...) }

func (plan *Plan) UnmarshalJSON(data []byte) error {
	type wire Plan
	var decoded wire
	if err := decodeKnownObject(data, &decoded); err != nil {
		return err
	}
	*plan = Plan(decoded)
	if fields, _, err := parseObject(data); err == nil {
		if raw, ok := fields["sections"]; ok {
			_, plan.sectionOrder, _ = parseObject(raw)
		}
	}
	baseline, err := marshalPlanWire(*plan)
	if err != nil {
		return err
	}
	return plan.state.capture(data, baseline)
}

func (plan Plan) MarshalJSON() ([]byte, error) {
	current, err := marshalPlanWire(plan)
	if err != nil {
		return nil, err
	}
	type wire Plan
	return plan.state.merge(current, knownJSONFields(reflect.TypeOf(wire{})))
}

func marshalPlanWire(plan Plan) ([]byte, error) {
	type wire Plan
	current, err := json.Marshal(wire(plan))
	if err != nil || len(plan.Sections) == 0 {
		return current, err
	}
	fields, order, err := parseObject(current)
	if err != nil {
		return nil, err
	}
	var sections bytes.Buffer
	sections.WriteByte('{')
	for i, key := range plan.SectionKeys() {
		if i > 0 {
			sections.WriteByte(',')
		}
		encodedKey, err := json.Marshal(key)
		if err != nil {
			return nil, err
		}
		encodedValue, err := json.Marshal(plan.Sections[key])
		if err != nil {
			return nil, err
		}
		sections.Write(encodedKey)
		sections.WriteByte(':')
		sections.Write(encodedValue)
	}
	sections.WriteByte('}')
	fields["sections"] = sections.Bytes()
	var out bytes.Buffer
	out.WriteByte('{')
	for i, key := range order {
		if i > 0 {
			out.WriteByte(',')
		}
		encodedKey, err := json.Marshal(key)
		if err != nil {
			return nil, err
		}
		out.Write(encodedKey)
		out.WriteByte(':')
		out.Write(fields[key])
	}
	out.WriteByte('}')
	return out.Bytes(), nil
}

func (template *Template) UnmarshalJSON(data []byte) error {
	type wire Template
	var decoded wire
	if err := decodeKnownObject(data, &decoded); err != nil {
		return err
	}
	*template = Template(decoded)
	baseline, err := json.Marshal(wire(*template))
	if err != nil {
		return err
	}
	return template.state.capture(data, baseline)
}

func (template Template) MarshalJSON() ([]byte, error) {
	type wire Template
	current, err := json.Marshal(wire(template))
	if err != nil {
		return nil, err
	}
	return template.state.merge(current, knownJSONFields(reflect.TypeOf(wire{})))
}

func (step *TemplateStep) UnmarshalJSON(data []byte) error {
	type wire TemplateStep
	var decoded wire
	if err := decodeKnownObject(data, &decoded); err != nil {
		return err
	}
	*step = TemplateStep(decoded)
	baseline, err := json.Marshal(wire(*step))
	if err != nil {
		return err
	}
	return step.state.capture(data, baseline)
}

func (step TemplateStep) MarshalJSON() ([]byte, error) {
	type wire TemplateStep
	current, err := json.Marshal(wire(step))
	if err != nil {
		return nil, err
	}
	return step.state.merge(current, knownJSONFields(reflect.TypeOf(wire{})))
}
