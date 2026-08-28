package main

import (
	"context"
	"fmt"
	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers/antigravity"
	"github.com/janekbaraniewski/openusage/internal/providers/command_code"
	"github.com/janekbaraniewski/openusage/internal/providers/opencode"
	"github.com/janekbaraniewski/openusage/internal/tui"
)

func main() {
	ag := antigravity.New()
	oc := opencode.New()
	cc := command_code.New()

	m := tui.NewTestModel()

	// 1. Antigravity Mohammed
	snapAg, _ := ag.Fetch(context.Background(), core.AccountConfig{ID: "antigravity-mohammed", Provider: "antigravity"})
	fmt.Println("================== ANTIGRAVITY MOHAMMED ==================")
	for _, line := range m.BuildTileGaugeLines(snapAg, 60) {
		fmt.Println(line)
	}

	// 2. OpenCode Mohammed
	snapOc, _ := oc.Fetch(context.Background(), core.AccountConfig{ID: "opencode-mohammed", Provider: "opencode"})
	fmt.Println("================== OPENCODE MOHAMMED ==================")
	for _, line := range m.BuildTileGaugeLines(snapOc, 60) {
		fmt.Println(line)
	}

	// 3. Command Code
	snapCc, _ := cc.Fetch(context.Background(), core.AccountConfig{ID: "command_code", Provider: "command_code", APIKeyEnv: "COMMAND_CODE_API_KEY"})
	fmt.Println("================== COMMAND CODE ==================")
	for _, line := range m.BuildTileGaugeLines(snapCc, 60) {
		fmt.Println(line)
	}
}
