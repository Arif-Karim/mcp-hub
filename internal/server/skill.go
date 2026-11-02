package server

import "github.com/Arif-Karim/mcp-hub/internal/skillfiles"

// Skill installs the embedded Claude Code skill (skillfiles.Manager).
type Skill interface {
	State() skillfiles.State
	Install() (skillfiles.State, error)
}
