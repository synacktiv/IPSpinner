package utils

import (
	"crypto/rand"
	"math/big"
)

// GenerateSecureRandomInt génère un nombre entier aléatoire entre 0 et max (exclusif) en utilisant crypto/rand.
func generateSecureRandomInt(max int) int {
	if max <= 0 {
		return 0
	}

	// Convertir max en un type big.Int pour l'utiliser avec crypto/rand
	n, err := rand.Int(rand.Reader, big.NewInt(int64(max)))
	if err != nil {
		return 0
	}

	// Convertir en int et retourner le résultat
	return int(n.Int64())
}
