package main

import (
	"flag"
	"fmt"
	"log"
	"math"
	"os"

	"gopkg.in/yaml.v3"
)

const (
	firstEntrySize = 12 + 177 // bytes
	subsequentSize = 177       // bytes
)

type Identity struct {
	Access string `yaml:"access"`
	Basic  struct {
		Password string `yaml:"password"`
	} `yaml:"basic"`
}

type Config struct {
	Identities map[string]Identity `yaml:"identities"`
}

func main() {
	var sizeInMiB int
	flag.IntVar(&sizeInMiB, "size", 1, "Output size in MiB (rounded up)")
	flag.Parse()

	targetSize := int64(sizeInMiB) * 1024 * 1024 // bytes
	if targetSize < firstEntrySize {
		log.Fatalf("Size too small: must be at least %d bytes", firstEntrySize)
	}

	remaining := targetSize - firstEntrySize
	additionalEntries := int(math.Ceil(float64(remaining) / float64(subsequentSize)))

	totalEntries := 1 + additionalEntries

	identities := make(map[string]Identity)
	for i := 1; i <= totalEntries; i++ {
		id := Identity{
			Access: "admin",
		}
		id.Basic.Password = "$6$iF6Fu0vfnbWPuRo2$noUY9LF33P15Id.evgTqF3vsOXvmyA19Y.PMGV2gqV7nvMgQkTo09B8iRdIq/9CUO4sSVc5MO.NvDMz1Zs7Aw1"
		identities[fmt.Sprintf("user%d", i)] = id
	}

	cfg := Config{
		Identities: identities,
	}

	encoder := yaml.NewEncoder(os.Stdout)
	encoder.SetIndent(4)
	if err := encoder.Encode(cfg); err != nil {
		log.Fatal(err)
	}
}

