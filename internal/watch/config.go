package watch

// WorkerConfig holds configuration for worker goroutines.
type WorkerConfig struct {
	WorktreeDir   string
	BaseBranch    string
	IssueLabels   string
	DockerEnabled bool
	DockerImage   string
}
