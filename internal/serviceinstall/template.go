package serviceinstall

import (
	"bytes"
	"fmt"
	"text/template"
)

// renderTemplate renders a template with the given SystemdUnit.
func renderTemplate(name, tmpl string, unit *SystemdUnit) string {
	t, err := template.New(name).Parse(tmpl)
	if err != nil {
		return fmt.Sprintf("; error rendering template: %v", err)
	}

	var buf bytes.Buffer
	if err := t.Execute(&buf, unit); err != nil {
		return fmt.Sprintf("; error executing template: %v", err)
	}

	return buf.String()
}
