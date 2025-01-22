package utils

const (
	LogFile                        = "ipspinner.log"
	SummarizeStateInterval         = 300
	MaxResourcePerFireProxInstance = 300
	IPSpinnerResponseHeaderPrefix  = "X-IPSpinner-"
)

// RandomWords are used for generating random sentences (to be stealthier when a string input is required => like for naming something)
// And also because its funnier to have a "real" sentence rather than fjeqrjagfdfdqs for a repository name :)
// Why are you still reading this?
// Thanks ChatGPT for generating a such nice list :)
var RandomWords = []string{
	"apple", "banana", "cat", "dog", "elephant", "fish", "gorilla", "hat", "icecream",
	"jacket", "kangaroo", "lemon", "monkey", "ninja", "orange", "penguin", "queen",
	"rabbit", "snake", "tiger", "umbrella", "vampire", "whale", "xylophone", "yak", "zebra",
}
