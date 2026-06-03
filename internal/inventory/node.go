// Package inventory defines the schema for cluster node inventory.
package inventory

// ServiceBinding represents a service running on a node bound to a specific role and port.
type ServiceBinding struct {
	Role string `yaml:"role" json:"role"`
	Port string `yaml:"port" json:"port"`
}

// Node represents a cluster node with support for multiple roles.
type Node struct {
	Hostname    string                   `yaml:"hostname" json:"hostname"`
	SSHUser     string                   `yaml:"ssh_user" json:"ssh_user"`
	Address     string                   `yaml:"address" json:"address"`
	Arch        string                   `yaml:"arch" json:"arch"`
	OS          string                   `yaml:"os" json:"os"`
	Role        string                   `yaml:"role" json:"role"`                     // deprecated: use Roles instead
	Roles       []string                 `yaml:"roles" json:"roles"`                   // new: list of roles
	Services    []ServiceBinding         `yaml:"services" json:"services"`             // service bindings per role
	Accelerator string                   `yaml:"accelerator" json:"accelerator"`
	VramGB      int                      `yaml:"vram_gb" json:"vram_gb"`
	RamGB       int                      `yaml:"ram_gb" json:"ram_gb"`
	DiskGB      int                      `yaml:"disk_gb" json:"disk_gb"`
	Labels      map[string]string        `yaml:"labels" json:"labels"`
	VRAMBudget  map[string]int           `yaml:"vram_budget" json:"vram_budget"`      // VRAM allocation per role
}

// PrimaryRole returns the primary role for backward compatibility.
// If Roles is populated, it returns the first role.
// Otherwise, it returns the deprecated Role field.
func (n *Node) PrimaryRole() string {
	if len(n.Roles) > 0 {
		return n.Roles[0]
	}
	return n.Role
}

// HasRole checks if the node has a given role.
func (n *Node) HasRole(role string) bool {
	for _, r := range n.Roles {
		if r == role {
			return true
		}
	}
	return false
}

// AddRole adds a role to the node if not already present.
func (n *Node) AddRole(role string) {
	if !n.HasRole(role) {
		n.Roles = append(n.Roles, role)
	}
}

// EffectiveRoles returns the list of effective roles, preferring Roles if populated,
// otherwise returning a single-element slice with the deprecated Role.
func (n *Node) EffectiveRoles() []string {
	if len(n.Roles) > 0 {
		return n.Roles
	}
	if n.Role != "" {
		return []string{n.Role}
	}
	return []string{}
}
