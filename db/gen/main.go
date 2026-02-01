package main

import (
	"gorm.io/gen"

	"github.com/quailyquaily/mister_morph/db/models"
)

//go:generate env GOCACHE=/tmp/go-build go run .
func main() {
	g := gen.NewGenerator(gen.Config{
		OutPath: "../query",
		Mode:    gen.WithDefaultQuery | gen.WithQueryInterface,
	})

	g.ApplyInterface(func(models.MemoryItemStore) {}, models.MemoryItem{})
	g.ApplyInterface(func(models.IdentityLinkStore) {}, models.IdentityLink{})

	g.Execute()
}
