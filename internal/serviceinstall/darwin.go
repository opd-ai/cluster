//go:build darwin
// +build darwin

package serviceinstall

import (
	"fmt"
	"os"
	"path/filepath"
)

// WriteDarwinUnit writes a launchd plist service file for macOS.
func WriteDarwinUnit(unit *SystemdUnit, dryRun bool) (string, error) {
	plistContent := renderLaunchdTemplate(unit)
	filename := fmt.Sprintf("com.opd.%s.plist", unit.Name)
	path := filepath.Join(os.Getenv("HOME"), "Library/LaunchAgents", filename)

	if dryRun {
		fmt.Printf("[DRY RUN] Would write %s:\n%s\n", path, plistContent)
		return path, nil
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", fmt.Errorf("failed to create launchd directory for %s: %w", path, err)
	}

	if err := os.WriteFile(path, []byte(plistContent), 0o644); err != nil {
		return "", fmt.Errorf("failed to write %s: %w", path, err)
	}

	return path, nil
}

// renderLaunchdTemplate renders a launchd plist from the template.
func renderLaunchdTemplate(unit *SystemdUnit) string {
	tmpl := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>com.opd.{{.Name}}</string>
  <key>ProgramArguments</key>
  <array>
   <string>{{.Executable}}</string>
{{range .Args}}    <string>{{.}}</string>
{{end}}  </array>
{{if .Environment}}  <key>EnvironmentVariables</key>
  <dict>
{{range $k, $v := .Environment}}    <key>{{$k}}</key>
   <string>{{$v}}</string>
{{end}}  </dict>
{{end}}  <key>RunAtLoad</key>
  <true/>
  <key>KeepAlive</key>
  <true/>
</dict>
</plist>
`

	return renderTemplate("launchd", tmpl, unit)
}
