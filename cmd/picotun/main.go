package main

import (
	"flag"
	"log"
	"strings"

	httpmux "github.com/amir6dev/PicoTun"
)

var version = "2.5.0"

func main() {
	showVersion := flag.Bool("version", false, "print version and exit")
	configPath := flag.String("config", "/etc/picotun/config.yaml", "path to config file")
	configShort := flag.String("c", "", "alias for -config")
	flag.Parse()

	if *showVersion {
		log.Printf("PTPlus %s", version)
		return
	}

	cfgPath := *configPath
	if *configShort != "" {
		cfgPath = *configShort
	}

	cfg, err := httpmux.LoadConfig(cfgPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	log.Printf("PTPlus %s — mode=%s profile=%s", version, cfg.Mode, cfg.Profile)

	switch strings.ToLower(strings.TrimSpace(cfg.Mode)) {
	case "server":
		srv := httpmux.NewServer(cfg)
		log.Fatal(srv.Start())

	case "client":
		cl := httpmux.NewClient(cfg)
		log.Fatal(cl.Start())

	default:
		log.Fatalf("unknown mode: %q (expected server/client)", cfg.Mode)
	}
}
