package youtrack

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// SavedQuery is one of the user's saved searches.
type SavedQuery struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Query string `json:"query"`
}

// User is whatever YouTrack gives us to name a person.
type User struct {
	Login    string `json:"login"`
	FullName string `json:"fullName"`
}

func (u *User) String() string {
	if u == nil {
		return ""
	}
	if u.FullName != "" {
		return u.FullName
	}
	return u.Login
}

// Issue is an issue. Only idReadable and summary are always populated; the
// rest depends on the fields spec used for the request.
type Issue struct {
	ID          string        `json:"idReadable"`
	Summary     string        `json:"summary"`
	Description string        `json:"description"`
	Created     int64         `json:"created"`
	Updated     int64         `json:"updated"`
	Resolved    *int64        `json:"resolved"`
	Reporter    *User         `json:"reporter"`
	Fields      []CustomField `json:"customFields"`
	Attachments []Attachment  `json:"attachments"`
	Links       []Link        `json:"links"`
}

// Field returns the display value of the named custom field, "" if absent.
func (i Issue) Field(name string) string {
	for _, f := range i.Fields {
		if strings.EqualFold(f.Name, name) {
			return f.String()
		}
	}
	return ""
}

// Attachment is a file on an issue or comment. URL is relative and pre-signed;
// run it through Client.AbsURL before handing it to a terminal.
type Attachment struct {
	Name     string `json:"name"`
	URL      string `json:"url"`
	Size     int64  `json:"size"`
	MimeType string `json:"mimeType"`
}

// LinkType names both directions of a relation.
type LinkType struct {
	Name           string `json:"name"`
	SourceToTarget string `json:"sourceToTarget"`
	TargetToSource string `json:"targetToSource"`
}

// Link is one group of related issues. YouTrack returns every link type of the
// issue, including the empty ones.
type Link struct {
	Direction string   `json:"direction"` // OUTWARD, INWARD, BOTH
	LinkType  LinkType `json:"linkType"`
	Issues    []Issue  `json:"issues"`
}

// Label is the direction-aware name of the relation ("depends on" vs
// "is required for").
func (l Link) Label() string {
	switch l.Direction {
	case "INWARD":
		if l.LinkType.TargetToSource != "" {
			return l.LinkType.TargetToSource
		}
	case "OUTWARD":
		if l.LinkType.SourceToTarget != "" {
			return l.LinkType.SourceToTarget
		}
	}
	if l.LinkType.Name != "" {
		return l.LinkType.Name
	}
	return "relates to"
}

// Comment is a comment on an issue.
type Comment struct {
	ID          string       `json:"id"`
	Text        string       `json:"text"`
	Created     int64        `json:"created"`
	Author      *User        `json:"author"`
	Attachments []Attachment `json:"attachments"`
}

// Change is one edit of a custom field, from an issue's activity stream.
type Change struct {
	Timestamp int64 `json:"timestamp"`
	Author    *User `json:"author"`
	Field     struct {
		// Presentation rather than name: name is the global field, which a
		// project can show under another label — Priority reads Prioridade.
		Name        string `json:"presentation"`
		CustomField struct {
			FieldType struct {
				ID string `json:"id"`
			} `json:"fieldType"`
		} `json:"customField"`
	} `json:"field"`
	Added   json.RawMessage `json:"added"`
	Removed json.RawMessage `json:"removed"`
}

// Before is what the field held until this edit, "" for nothing.
func (c Change) Before() string { return c.render(c.Removed) }

// After is what this edit left in the field, "" for nothing.
func (c Change) After() string { return c.render(c.Added) }

// render reads one side of an edit. A date, a period and an integer all arrive
// as a bare number here, so only the field's type tells them apart.
func (c Change) render(raw json.RawMessage) string {
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return ""
	}
	n, isNum := v.(float64)
	switch t := c.Field.CustomField.FieldType.ID; {
	case isNum && (t == "date" || t == "date and time"):
		return time.UnixMilli(int64(n)).Format("2006-01-02 15:04")
	case isNum && t == "period":
		return hoursMinutes(int(n))
	}
	return renderAny(v)
}

// hoursMinutes renders a period given in minutes.
//
// ponytail: YouTrack says 2d where this says 16h, because its day is an admin's
// workday setting. Read /api/admin/timeTrackingSettings if the two must match.
func hoursMinutes(n int) string {
	h, m := n/60, n%60
	switch {
	case h == 0:
		return fmt.Sprintf("%dm", m)
	case m == 0:
		return fmt.Sprintf("%dh", h)
	}
	return fmt.Sprintf("%dh%dm", h, m)
}

// CustomField is a project-defined field. Value is kept raw because its shape
// depends on the field type, which varies per YouTrack instance — see the
// dynamic-fields invariant in CLAUDE.md.
type CustomField struct {
	Name  string          `json:"name"`
	Type  string          `json:"$type"`
	Value json.RawMessage `json:"value"`
}

// String renders any custom field value into a display string.
func (f CustomField) String() string {
	if len(f.Value) == 0 || string(f.Value) == "null" {
		return ""
	}
	var v any
	if err := json.Unmarshal(f.Value, &v); err != nil {
		return ""
	}
	switch f.Type {
	case "DateIssueCustomField":
		if ms, ok := v.(float64); ok {
			return time.UnixMilli(int64(ms)).Format("2006-01-02 15:04")
		}
	case "PeriodIssueCustomField":
		if m, ok := v.(map[string]any); ok {
			if p, ok := m["presentation"].(string); ok {
				return p
			}
		}
	}
	return renderAny(v)
}

// nameKeys are the object keys that carry a human label, most specific first.
var nameKeys = []string{"name", "fullName", "login", "presentation", "text"}

func renderAny(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case bool:
		if t {
			return "yes"
		}
		return "no"
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case []any:
		parts := make([]string, 0, len(t))
		for _, e := range t {
			if s := renderAny(e); s != "" {
				parts = append(parts, s)
			}
		}
		return strings.Join(parts, ", ")
	case map[string]any:
		for _, k := range nameKeys {
			if s := renderAny(t[k]); s != "" {
				return s
			}
		}
		return ""
	default:
		return fmt.Sprint(t)
	}
}
