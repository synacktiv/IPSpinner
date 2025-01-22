package utils

// Removes the given element in the slice (removes it each time it appears)
func DeleteElementFromSlice[T comparable](slice []T, element T) []T {
	newSlice := []T{}

	for _, e := range slice {
		if e != element {
			newSlice = append(newSlice, e)
		}
	}

	return newSlice
}

// Returns a random element from the slice
func RandomElementInSlice[T any](slice []T) T {
	randomIndex := generateSecureRandomInt(len(slice))

	return slice[randomIndex]
}

// Takes as input a list and a maxLength and subdivises the input list into a list of sublists with a maximym length of maxLength
func SubdiviseSlice[T any](slice []T, maxSubSliceLength int) [][]T {
	allSubSlices := [][]T{}
	currentSubSlice := []T{}

	for _, element := range slice {
		currentSubSlice = append(currentSubSlice, element)

		if len(currentSubSlice) >= maxSubSliceLength {
			allSubSlices = append(allSubSlices, currentSubSlice)

			currentSubSlice = []T{}
		}
	}

	if len(currentSubSlice) > 0 {
		allSubSlices = append(allSubSlices, currentSubSlice)
	}

	return allSubSlices
}
