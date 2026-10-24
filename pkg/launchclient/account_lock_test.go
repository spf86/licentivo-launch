package launchclient

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestAccountLockCoordinatesProcessesAndRecoversAfterExit(t *testing.T) {
	if path := os.Getenv("LAUNCH_ACCOUNT_LOCK_TEST_PATH"); path != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		unlock, err := lockAccountFile(ctx, path)
		if err != nil {
			t.Fatal(err)
		}
		defer unlock()
		fmt.Println("account-lock-acquired")
		if os.Getenv("LAUNCH_ACCOUNT_LOCK_TEST_HOLD") == "1" {
			time.Sleep(30 * time.Second)
		}
		return
	}
	path := filepath.Join(t.TempDir(), "account.lock")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	unlock, err := lockAccountFile(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	start := func(hold bool) (*exec.Cmd, *bufio.Reader) {
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestAccountLockCoordinatesProcessesAndRecoversAfterExit$", "-test.count=1")
		cmd.Env = append(os.Environ(), "LAUNCH_ACCOUNT_LOCK_TEST_PATH="+path)
		if hold {
			cmd.Env = append(cmd.Env, "LAUNCH_ACCOUNT_LOCK_TEST_HOLD=1")
		}
		stdout, e := cmd.StdoutPipe()
		if e != nil {
			t.Fatal(e)
		}
		if e = cmd.Start(); e != nil {
			t.Fatal(e)
		}
		t.Cleanup(func() { _ = cmd.Process.Kill() })
		return cmd, bufio.NewReader(stdout)
	}
	child, reader := start(false)
	acquired := make(chan string, 1)
	go func() { line, _ := reader.ReadString('\n'); acquired <- line }()
	select {
	case <-acquired:
		unlock()
		t.Fatal("another process entered the held account lock")
	case <-time.After(100 * time.Millisecond):
	}
	unlock()
	select {
	case line := <-acquired:
		if line != "account-lock-acquired\n" {
			t.Fatal(line)
		}
	case <-ctx.Done():
		t.Fatal("waiting process did not acquire account lock")
	}
	if err = child.Wait(); err != nil {
		t.Fatal(err)
	}
	holder, output := start(true)
	line, err := output.ReadString('\n')
	if err != nil || line != "account-lock-acquired\n" {
		t.Fatal("holder failed", err, line)
	}
	if err = holder.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = holder.Wait()
	next, err := lockAccountFile(ctx, path)
	if err != nil {
		t.Fatal("dead process retained account lock", err)
	}
	next()
}
