package main

import "testing"

func TestCommandConfigurationSelection(t *testing.T) {
	for _, test := range []struct {
		args                     []string
		command, archive, config string
	}{
		{nil, "serve", "", ""},
		{[]string{"serve"}, "serve", "", ""},
		{[]string{"--config", "private/config.yaml"}, "serve", "", "private/config.yaml"},
		{[]string{"--config", "private/config.yaml", "check"}, "check", "", "private/config.yaml"},
		{[]string{"init", "--config=private/config.yml"}, "init", "", "private/config.yml"},
		{[]string{"dev", "--config", "private/config.yaml"}, "dev", "", "private/config.yaml"},
		{[]string{"backup", "backup file.launchbackup", "--config", "private/config.yaml"}, "backup", "backup file.launchbackup", "private/config.yaml"},
		{[]string{"restore", "--config", "recovery.secrets.yaml", "backup.launchbackup"}, "restore", "backup.launchbackup", "recovery.secrets.yaml"},
		{[]string{"backup", "--", "--backup.launchbackup"}, "backup", "--backup.launchbackup", ""},
	} {
		command, archive, path, err := parseCommand(test.args)
		if err != nil || command != test.command || archive != test.archive || path != test.config {
			t.Fatalf("wrong command selection for %v", test.args)
		}
	}
	for _, args := range [][]string{{"--config"}, {"--config="}, {"serve", "--config", "--unknown"}, {"--unknown"}, {"serve", "extra"}, {"backup"}, {"restore", "one", "two"}, {"--config=a.yaml", "--config=b.yaml"}, {"unknown"}} {
		if _, _, _, err := parseCommand(args); err == nil {
			t.Fatalf("invalid arguments accepted: %v", args)
		}
	}
}
