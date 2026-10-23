package main

import (
	"errors"
	"strings"
)

const commandUsage = "usage: launch [init|serve|dev|check|keygen|version] [--config PATH] or launch [backup|restore] PATH [--config PATH]"

func parseCommand(args []string) (command, archivePath, configPath string, err error) {
	var positional []string
	options := true
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case options && arg == "--":
			options = false
		case options && (arg == "--config" || strings.HasPrefix(arg, "--config=")):
			if configPath != "" {
				return "", "", "", errors.New(commandUsage)
			}
			if arg == "--config" {
				i++
				if i >= len(args) || strings.HasPrefix(args[i], "--") {
					return "", "", "", errors.New(commandUsage)
				}
				configPath = args[i]
			} else {
				configPath = strings.TrimPrefix(arg, "--config=")
			}
			if configPath == "" {
				return "", "", "", errors.New(commandUsage)
			}
		case options && strings.HasPrefix(arg, "-"):
			return "", "", "", errors.New(commandUsage)
		default:
			positional = append(positional, arg)
		}
	}
	command = "serve"
	if len(positional) > 0 {
		command = positional[0]
	}
	switch command {
	case "backup", "restore":
		if len(positional) != 2 {
			return "", "", "", errors.New(commandUsage)
		}
		archivePath = positional[1]
	case "init", "serve", "dev", "check", "keygen", "version":
		if len(positional) > 1 {
			return "", "", "", errors.New(commandUsage)
		}
	default:
		return "", "", "", errors.New(commandUsage)
	}
	return command, archivePath, configPath, nil
}
