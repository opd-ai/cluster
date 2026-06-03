// Package serviceinstall handles writing systemd unit files for service daemon management.
package serviceinstall

import (
	"fmt"
	"os"
	"path/filepath"
)

// SystemdUnit represents a systemd service unit.
type SystemdUnit struct {
	Name        string
	Description string
	Executable  string
	Args        []string
	Environment map[string]string
	Dependencies []string
}

// WriteLinuxUnit writes a systemd service unit file to the system directory.
func WriteLinuxUnit(unit *SystemdUnit, dryRun bool) (string, error) {
	unitContent := renderSystemdTemplate(unit)
	filename := fmt.Sprintf("%s.service", unit.Name)
	path := filepath.Join("/etc/systemd/system", filename)

	if dryRun {
		fmt.Printf("[DRY RUN] Would write %s:\n%s\n", path, unitContent)
		return path, nil
	}

	if err := os.WriteFile(path, []byte(unitContent), 0o644); err != nil {
		return "", fmt.Errorf("failed to write %s: %w", path, err)
	}

	return path, nil
}

// renderSystemdTemplate renders a systemd unit file from the template.
func renderSystemdTemplate(unit *SystemdUnit) string {
	tmpl := `[Unit]
Description={{.Description}}
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=root
{{range $k, $v := .Environment}}
Environment="{{$k}}={{$v}}"
{{end}}
ExecStart={{.Executable}}{{range .Args}} {{.}}{{end}}
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
`

	return renderTemplate("systemd", tmpl, unit)
}
