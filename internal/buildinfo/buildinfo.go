package buildinfo

import "fmt"

var (
	Version = "dev"
	Commit  = "unknown"
	Date    = "unknown"
)

func String() string { return fmt.Sprintf("herdlord %s (%s, %s)", Version, Commit, Date) }
