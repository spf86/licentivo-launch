package ports

import "context"

type TranslationUsage struct {
	CharacterCount uint64
	CharacterLimit uint64
}

// Translator is the small provider-neutral surface needed by Launch. The
// current implementation is DeepL, while this interface keeps network calls
// replaceable in deterministic tests.
type Translator interface {
	Usage(context.Context) (TranslationUsage, error)
	Translate(context.Context, string, string, []string) ([]string, error)
}
