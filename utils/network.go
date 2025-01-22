package utils

import (
	"errors"
	"math/big"
	"math/rand"
	"net"
	"time"
)

const ExpectedBitSize = 128
const PaddingSize = 16

// Returns a random IP from the CIDR
func RandomIPFromCIDR(cidr string) (net.IP, error) {
	_, ipNet, err := net.ParseCIDR(cidr)
	if err != nil {
		return nil, err
	}

	if ipNet.IP.To4() != nil {
		return randomIPv4FromCIDR(cidr)
	}

	return randomIPv6FromCIDR(cidr)
}

// randomIPv6FromCIDR generates a random IPv6 address from the given CIDR.
func randomIPv6FromCIDR(cidr string) (net.IP, error) {
	_, ipv6Net, err := net.ParseCIDR(cidr)
	if err != nil {
		return nil, errors.New("invalid CIDR")
	}

	// Get the network start IP
	startIP := ipv6Net.IP
	startBytes := startIP.To16()

	if startBytes == nil {
		return nil, errors.New("invalid start IP in CIDR")
	}

	startInt := new(big.Int).SetBytes(startBytes)

	// Calculate the network size
	maskSize, totalBits := ipv6Net.Mask.Size()
	if totalBits != ExpectedBitSize {
		return nil, errors.New("invalid IPv6 network mask")
	}

	networkSize := ExpectedBitSize - maskSize

	// Calculate the maximum IP in the range (end IP)
	maxAdd := new(big.Int).Lsh(big.NewInt(1), uint(networkSize))
	endInt := new(big.Int).Add(startInt, maxAdd)
	endInt.Sub(endInt, big.NewInt(1))

	// Generate a random number in the range [startInt, endInt]
	diff := new(big.Int).Sub(endInt, startInt)
	randInt := new(big.Int).Rand(rand.New(rand.NewSource(time.Now().UnixNano())), new(big.Int).Add(diff, big.NewInt(1))) //nolint:gosec
	randInt.Add(randInt, startInt)

	// Convert back to IP
	randBytes := randInt.Bytes()
	if len(randBytes) < PaddingSize {
		padded := make([]byte, PaddingSize)
		copy(padded[PaddingSize-len(randBytes):], randBytes)
		randBytes = padded
	}

	randomIP := net.IP(randBytes)

	return randomIP, nil
}

// Returns a random IPv4 from the CIDR
func randomIPv4FromCIDR(cidr string) (net.IP, error) {
	_, ipNet, err := net.ParseCIDR(cidr)

	if err != nil {
		return nil, err
	}

	// Convert network IP to 32-bit integer
	start := ipNet.IP.To4()
	startInt := int(start[0])<<24 | int(start[1])<<16 | int(start[2])<<8 | int(start[3]) //nolint:revive

	// Calculate the number of possible addresses in the CIDR
	ones, bits := ipNet.Mask.Size()
	numAddresses := 1 << uint(bits-ones)

	// Generate a random offset within the range of possible addresses
	offset := generateSecureRandomInt(numAddresses)

	// Calculate the final IP address by adding the offset to the starting IP
	resultInt := startInt + offset
	result := make(net.IP, 4)         //nolint:gomnd
	result[0] = byte(resultInt >> 24) //nolint:gomnd
	result[1] = byte(resultInt >> 16) //nolint:gomnd
	result[2] = byte(resultInt >> 8)  //nolint:gomnd
	result[3] = byte(resultInt)

	return result, nil
}
