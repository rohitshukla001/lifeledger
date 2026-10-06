package version

var (
	Version = "dev"
	Commit  = "none"
)

func String() string {
	return Version + " (" + Commit + ")"
}
