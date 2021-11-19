package util

func ContainString(ss []string, s string) bool {
	for _, as := range ss {
		if as == s {
			return true
		}
	}
	return false
}
