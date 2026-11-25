package app

import codexservice "github.com/wsyangzy/Vane/internal/providers/codex/service"

// Compaction is selected before Vane routes a request. One client entry owns
// the choice, regardless of the channels reachable through its routing profile.
func (a *App) codexClientConfigOptions(catalog *codexservice.ClientModelCatalog, compactionChoices ...string) (codexservice.ClientConfigOptions, error) {
	choice := ""
	if len(compactionChoices) > 0 {
		choice = compactionChoices[0]
	}
	mode, err := codexservice.NormalizeClientCompactionMode(choice)
	if err != nil {
		return codexservice.ClientConfigOptions{}, err
	}
	return codexservice.ClientConfigOptions{ModelCatalog: catalog, CompactionMode: mode}, nil
}
