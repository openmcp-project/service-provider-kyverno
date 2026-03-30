package v1alpha1

func GetReleaseName(name string) string {
	return "kyverno-" + name
}
