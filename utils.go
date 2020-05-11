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

// Simple (if not efficient) function to determine if
// two string arrays contain the same data
func StringArrayEquals(a1, a2 []string) bool {
	if a1 == nil && a2 == nil {
		return true
	}

	if len(a1) != len(a2) {
		return false
	}

	for _, s := range a1 {
		if !StringArrayContains(a2, s) {
			return false
		}
	}

	return true
}

// returns an array of strings that are missing
func FindMissingStrings(strings, expectedStrings []string) []string {
	if strings == nil || len(strings) == 0 {
		return expectedStrings
	}

	var missingStrings = make([]string, len(expectedStrings))

	var numStrings = 0
	for _, s := range expectedStrings {
		if !StringArrayContains(strings, s) {
			missingStrings[numStrings] = s
			numStrings++
		}
	}

	if numStrings > 0 {
		return missingStrings[0:numStrings]
	} else {
		return nil
	}
}
