package main

import (
	"os"

	"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/modelcli"
)

func main() { os.Exit(modelcli.Run(os.Args[1:], os.Stdout, os.Stderr)) }
