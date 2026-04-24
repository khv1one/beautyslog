package beautyslog

import (
	"fmt"
	"strings"
)

// Field represents a single output component.
type Field int

// Field constants.
const (
	FieldTime Field = iota
	FieldLevel
	FieldMessage
	FieldSource
	FieldAttrs
)

var fieldNames = map[string]Field{
	"time":    FieldTime,
	"level":   FieldLevel,
	"message": FieldMessage,
	"source":  FieldSource,
	"attrs":   FieldAttrs,
}

// Layout is an ordered list of fields.
type Layout []Field

// DefaultLayout is the default output order.
var DefaultLayout = Layout{FieldTime, FieldSource, FieldLevel, FieldMessage, FieldAttrs}

// TemplateSegment is a single piece of a parsed template.
type TemplateSegment struct {
	IsField bool
	Field   Field
	Literal string
}

// ParsedTemplate is the parsed representation of a user template string.
type ParsedTemplate []TemplateSegment

// ParseTemplate converts a format string like "[{level}] {time} {message} {attrs}"
// into a ParsedTemplate. Supported tokens: {time}, {level}, {message}, {source}, {attrs}.
func ParseTemplate(s string) (ParsedTemplate, error) {
	var segs ParsedTemplate
	var lit strings.Builder

	for i := 0; i < len(s); i++ {
		if s[i] != '{' {
			lit.WriteByte(s[i])
			continue
		}
		// flush literal
		if lit.Len() > 0 {
			segs = append(segs, TemplateSegment{IsField: false, Literal: lit.String()})
			lit.Reset()
		}
		// find closing brace
		j := strings.IndexByte(s[i+1:], '}')
		if j == -1 {
			return nil, fmt.Errorf("parse template: unmatched '{' at position %d", i)
		}
		token := s[i+1 : i+1+j]
		f, ok := fieldNames[token]
		if !ok {
			return nil, fmt.Errorf("parse template: unknown field %q at position %d", token, i)
		}
		segs = append(segs, TemplateSegment{IsField: true, Field: f})
		i += j + 1
	}
	if lit.Len() > 0 {
		segs = append(segs, TemplateSegment{IsField: false, Literal: lit.String()})
	}
	return segs, nil
}
