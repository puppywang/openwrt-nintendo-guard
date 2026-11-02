package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	guard "github.com/puppywang/openwrt-nintendo-guard/internal/nintendoguard"
)

var version = "dev"

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	args := os.Args[1:]
	if len(args) == 0 {
		return fmt.Errorf("usage: nintendo-guard serve|status|set|firewall|rules|validate|version|rpc")
	}
	switch args[0] {
	case "cleanup":
		return guard.Cleanup()
	case "version":
		fmt.Println(version)
		return nil
	case "validate":
		var c guard.Config
		if err := json.NewDecoder(io.LimitReader(os.Stdin, guard.ConfigInputLimit)).Decode(&c); err != nil {
			return err
		}
		c, err := c.Validate()
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(c)
	case "serve":
		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()
		return guard.Serve(ctx)
	case "rules":
		c, err := guard.ReadConfig()
		if err != nil {
			return err
		}
		fmt.Print(c.Rules())
		return nil
	case "firewall":
		if _, err := guard.Call("reapply", strings.NewReader("{}")); err == nil {
			return nil
		}
		c, err := guard.ReadConfig()
		if err != nil {
			return err
		}
		return guard.ApplyFirewall(c)
	case "status", "set":
		var input io.Reader
		if args[0] == "set" {
			input = io.LimitReader(os.Stdin, guard.ConfigInputLimit)
		}
		b, err := guard.Call(args[0], input)
		if err != nil {
			return err
		}
		fmt.Print(string(b))
		return nil
	case "rpc":
		if len(args) == 2 && args[1] == "list" {
			fmt.Print(`{"status":{},"set":{"devices":[],"upstream":""}}`)
			return nil
		}
		if len(args) != 3 || args[1] != "call" || (args[2] != "status" && args[2] != "set") {
			return fmt.Errorf("invalid rpc method")
		}
		var input io.Reader
		if args[2] == "set" {
			input = io.LimitReader(os.Stdin, guard.ConfigInputLimit)
		}
		b, err := guard.Call(args[2], input)
		if err != nil {
			c, _ := guard.ReadConfig()
			return json.NewEncoder(os.Stdout).Encode(map[string]any{"running": false, "config": c, "error": err.Error()})
		}
		fmt.Print(string(b))
		return nil
	}
	return fmt.Errorf("unknown command")
}
