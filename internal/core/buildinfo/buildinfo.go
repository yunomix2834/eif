package buildinfo

import "fmt"

var (
	Version            = "dev"
	BackendCommit      = "unknown"
	FrontendCommit     = "unknown"
	OrchestratorCommit = "unknown"
	BuildTime          = "unknown"
)

func String() string {
	return fmt.Sprintf(
		"EIF %s\nbackend: %s\nfrontend: %s\norchestrator: %s\nbuilt: %s",
		Version,
		BackendCommit,
		FrontendCommit,
		OrchestratorCommit,
		BuildTime,
	)
}
