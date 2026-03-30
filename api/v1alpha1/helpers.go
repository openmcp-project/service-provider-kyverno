package v1alpha1

// GetReleaseName returns the Helm release name for the given Kyverno instance.
func GetReleaseName(name string) string {
	return "kyverno-" + name
}
