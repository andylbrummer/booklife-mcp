package main

import (
	"fmt"
	"github.com/user/booklife-mcp/internal/config"
	"os"
)

func main() {
	cfg, err := config.Load("/home/beagle/books/booklife.kdl")
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Success! LocalBookstores enabled=%v, stores=%d\n",
		cfg.Providers.LocalBookstores.Enabled,
		len(cfg.Providers.LocalBookstores.Stores))
	for i, s := range cfg.Providers.LocalBookstores.Stores {
		fmt.Printf("Store %d: ID=%s Name=%s Location=%s\n", i, s.ID, s.Name, s.Location)
	}
}
