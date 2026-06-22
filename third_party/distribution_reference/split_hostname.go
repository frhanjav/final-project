package reference

// SplitHostname was removed from newer github.com/distribution/reference
// releases, but github.com/docker/distribution still calls it through its
// deprecated compatibility package.
func SplitHostname(named Named) (string, string) {
	return Domain(named), Path(named)
}
