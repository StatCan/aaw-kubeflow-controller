package main

// StringArrayContains checks if a value is within a string array.
func StringArrayContains(strings []string, str string) bool {
	for _, cstr := range strings {
		if str == cstr {
			return true
		}
	}

	return false
}
