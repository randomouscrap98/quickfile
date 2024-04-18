package quickfile

import (
	"strings"
)

// Function to generate placeholders for SQL query
func sliceToPlaceholder[T any](slice []T) string {
	var sb strings.Builder
	ph := []byte("?,")
	phlast := []byte("?")
	for i := range slice {
		if i == len(slice)-1 {
			sb.Write(phlast)
		} else {
			sb.Write(ph)
		}
	}
	return sb.String()
}

func sliceToAny[T any](slice []T) []any {
	anys := make([]any, len(slice))
	for i := range anys {
		anys[i] = slice[i]
	}
	return anys
}

// A very slow and memory ineficient way to get the distinct
// set of items from a slice. Order is not preserved
func sliceDistinct[T comparable](slice []T) []T {
	set := make(map[T]bool)
	for _, item := range slice {
		set[item] = true
	}
	result := make([]T, len(set))
	i := 0
	for k := range set {
		result[i] = k
		i += 1
	}
	return result
}

func StringUpTo(delim, base string) string {
	end := strings.Index(base, delim)
	if end >= 0 {
		return base[:end]
	}
	return base
}

func anyStartsWith(thing string, things []string) bool {
	for _, s := range things {
		if strings.HasPrefix(thing, s) {
			return true
		}
	}
	return false
}
