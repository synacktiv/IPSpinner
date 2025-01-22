package utils

import (
	"fmt"
	"math/rand"

	"github.com/google/uuid"
)

// Generates a random password (at least one character of each class) (min size = 4)
func GenerateRandomPassword(size int) string {
	const (
		lowerChars   = "abcdefghijklmnopqrstuvwxyz"
		upperChars   = "ABCDEFGHIJKLMNOPQRSTUVWXYZ"
		numberChars  = "0123456789"
		specialChars = "!.*"
	)

	const allChars = lowerChars + upperChars + numberChars + specialChars

	password := randomChar(lowerChars) + randomChar(upperChars) + randomChar(numberChars) + randomChar(specialChars)

	for i := 0; i < (size - 4); i++ { //nolint:gomnd
		password += randomChar(allChars)
	}

	return shuffle(password)
}

// GenerateRandomSentence generates a random English sentence with X words
func GenerateRandomSentence(size int) string {
	var sentence string

	for i := 0; i < size; i++ {
		if i > 0 {
			sentence += " "
		}

		wordIndex := generateSecureRandomInt(len(RandomWords))
		sentence += RandomWords[wordIndex]
	}

	// Capitalize the first letter of the sentence (it's more beautiful, isn't it?)
	sentence = string(sentence[0]-32) + sentence[1:] //nolint:gomnd

	return sentence
}

func GenerateUUIDv4() string {
	return uuid.New().String()
}

func randomChar(str string) string {
	return string(str[generateSecureRandomInt(len(str))])
}

func shuffle(str string) string {
	inRune := []rune(str)

	rand.Shuffle(len(inRune), func(i, j int) {
		inRune[i], inRune[j] = inRune[j], inRune[i]
	})

	return string(inRune)
}

func PrepareBearerHeader(token string) string {
	return fmt.Sprintf("Bearer %s", token)
}
