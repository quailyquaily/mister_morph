package consolecmd

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/spf13/viper"
)

func TestManagedRuntimeSupervisorReloadWaitsForPreviousChildren(t *testing.T) {
	oldStarted := make(chan struct{})
	oldCanceled := make(chan struct{})
	releaseOld := make(chan struct{})
	oldCleaned := make(chan struct{})
	newStarted := make(chan struct{})
	newExited := make(chan struct{})
	var newStartedBeforeCleanup atomic.Bool

	supervisor := newManagedRuntimeSupervisor(nil, false, false)
	supervisor.pendingPrepared = &managedRuntimePrepared{
		reader: viper.New(),
		kinds:  []string{"old"},
		children: []managedPreparedRuntime{{
			kind: "old",
			run: func(ctx context.Context) error {
				close(oldStarted)
				<-ctx.Done()
				close(oldCanceled)
				<-releaseOld
				return ctx.Err()
			},
			cleanup: func() { close(oldCleaned) },
		}},
	}
	if err := supervisor.Start(context.Background(), nil); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	select {
	case <-oldStarted:
	case <-time.After(time.Second):
		t.Fatal("old managed runtime did not start")
	}

	next := &managedRuntimePrepared{
		reader: viper.New(),
		kinds:  []string{"new"},
		children: []managedPreparedRuntime{{
			kind: "new",
			run: func(ctx context.Context) error {
				select {
				case <-oldCleaned:
				default:
					newStartedBeforeCleanup.Store(true)
				}
				close(newStarted)
				<-ctx.Done()
				close(newExited)
				return ctx.Err()
			},
		}},
	}
	applyDone := make(chan error, 1)
	go func() {
		applyDone <- supervisor.ApplyPrepared(next)
	}()

	select {
	case <-oldCanceled:
	case <-time.After(time.Second):
		t.Fatal("reload did not cancel the old managed runtime")
	}
	startedEarly := false
	select {
	case <-newStarted:
		startedEarly = true
	case <-time.After(100 * time.Millisecond):
	}
	close(releaseOld)
	select {
	case err := <-applyDone:
		if err != nil {
			t.Fatalf("ApplyPrepared() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("ApplyPrepared() did not return after the old runtime exited")
	}
	select {
	case <-newStarted:
	case <-time.After(time.Second):
		t.Fatal("new managed runtime did not start")
	}

	supervisor.Close()
	select {
	case <-newExited:
	case <-time.After(time.Second):
		t.Fatal("new managed runtime did not exit on close")
	}
	if startedEarly {
		t.Error("new managed runtime started before the old runtime exited")
	}
	if newStartedBeforeCleanup.Load() {
		t.Error("new managed runtime started before old cleanup completed")
	}
}

func TestManagedRuntimeSupervisorCloseWaitsForChildren(t *testing.T) {
	started := make(chan struct{})
	canceled := make(chan struct{})
	release := make(chan struct{})
	cleaned := make(chan struct{})

	supervisor := newManagedRuntimeSupervisor(nil, false, false)
	supervisor.pendingPrepared = &managedRuntimePrepared{
		reader: viper.New(),
		kinds:  []string{"test"},
		children: []managedPreparedRuntime{{
			kind: "test",
			run: func(ctx context.Context) error {
				close(started)
				<-ctx.Done()
				close(canceled)
				<-release
				return ctx.Err()
			},
			cleanup: func() { close(cleaned) },
		}},
	}
	if err := supervisor.Start(context.Background(), nil); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("managed runtime did not start")
	}

	closeDone := make(chan struct{})
	go func() {
		supervisor.Close()
		close(closeDone)
	}()
	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Fatal("Close() did not cancel the managed runtime")
	}
	returnedEarly := false
	select {
	case <-closeDone:
		returnedEarly = true
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	select {
	case <-closeDone:
	case <-time.After(time.Second):
		t.Fatal("Close() did not return after the managed runtime exited")
	}
	select {
	case <-cleaned:
	default:
		t.Error("Close() returned before managed runtime cleanup completed")
	}
	if returnedEarly {
		t.Error("Close() returned before the managed runtime exited")
	}
}

func TestManagedRuntimeSupervisorReloadDisablesTelegramMissingToken(t *testing.T) {
	local := &consoleLocalRuntime{managedRuntimeRunning: map[string]bool{}}
	local.SetManagedRuntimeRunning("telegram", true)
	supervisor := newManagedRuntimeSupervisor(local, false, false)

	current := viper.New()
	current.Set("console.managed_runtimes", []string{"telegram"})
	current.Set("telegram.bot_token", "old-token")
	supervisor.parentCtx = context.Background()
	supervisor.configReader = current
	supervisor.kinds = []string{"telegram"}

	next := viper.New()
	next.Set("console.managed_runtimes", []string{"telegram"})

	err := supervisor.ReloadConfig(next)
	if err != nil {
		t.Fatalf("ReloadConfig() error = %v, want nil", err)
	}
	if supervisor.configReader != next {
		t.Fatal("configReader was not updated")
	}
	if got := supervisor.configReader.GetString("telegram.bot_token"); got != "" {
		t.Fatalf("configReader.telegram.bot_token = %q, want empty", got)
	}
	if len(supervisor.kinds) != 0 {
		t.Fatalf("kinds = %#v, want empty", supervisor.kinds)
	}
	if local.isManagedRuntimeRunning("telegram") {
		t.Fatal("telegram running = true, want false")
	}
}

func TestManagedRuntimeSupervisorReloadDisablesSlackMissingTokens(t *testing.T) {
	cases := []struct {
		name    string
		setNext func(*viper.Viper)
	}{
		{name: "missing bot token"},
		{
			name: "missing app token",
			setNext: func(v *viper.Viper) {
				v.Set("slack.bot_token", "xoxb-test")
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			local := &consoleLocalRuntime{managedRuntimeRunning: map[string]bool{}}
			local.SetManagedRuntimeRunning("slack", true)
			supervisor := newManagedRuntimeSupervisor(local, false, false)
			supervisor.parentCtx = context.Background()
			supervisor.configReader = viper.New()
			supervisor.kinds = []string{"slack"}

			next := viper.New()
			next.Set("console.managed_runtimes", []string{"slack"})
			if tc.setNext != nil {
				tc.setNext(next)
			}

			err := supervisor.ReloadConfig(next)
			if err != nil {
				t.Fatalf("ReloadConfig() error = %v, want nil", err)
			}
			if supervisor.configReader != next {
				t.Fatal("configReader was not updated")
			}
			if len(supervisor.kinds) != 0 {
				t.Fatalf("kinds = %#v, want empty", supervisor.kinds)
			}
			if local.isManagedRuntimeRunning("slack") {
				t.Fatal("slack running = true, want false")
			}
		})
	}
}

func TestManagedRuntimeSupervisorPrepareSkipsSlackMissingTokens(t *testing.T) {
	cases := []struct {
		name    string
		setNext func(*viper.Viper)
	}{
		{name: "missing bot token"},
		{
			name: "missing app token",
			setNext: func(v *viper.Viper) {
				v.Set("slack.bot_token", "xoxb-token")
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			supervisor := newManagedRuntimeSupervisor(nil, false, false)
			reader := viper.New()
			reader.Set("console.managed_runtimes", []string{"slack"})
			if tc.setNext != nil {
				tc.setNext(reader)
			}

			prepared, err := supervisor.PrepareReload(reader)
			if err != nil {
				t.Fatalf("PrepareReload() error = %v, want nil", err)
			}
			if len(prepared.kinds) != 0 {
				t.Fatalf("prepared.kinds = %#v, want empty", prepared.kinds)
			}
			if len(prepared.children) != 0 {
				t.Fatalf("prepared.children len = %d, want 0", len(prepared.children))
			}
		})
	}
}

func TestManagedRuntimeSupervisorPrepareSkipsLarkMissingCredentials(t *testing.T) {
	cases := []struct {
		name    string
		setNext func(*viper.Viper)
	}{
		{name: "missing app id"},
		{
			name: "missing app secret",
			setNext: func(v *viper.Viper) {
				v.Set("lark.app_id", "cli_test")
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			supervisor := newManagedRuntimeSupervisor(nil, false, false)
			reader := viper.New()
			reader.Set("console.managed_runtimes", []string{"lark"})
			if tc.setNext != nil {
				tc.setNext(reader)
			}

			prepared, err := supervisor.PrepareReload(reader)
			if err != nil {
				t.Fatalf("PrepareReload() error = %v, want nil", err)
			}
			if len(prepared.kinds) != 0 {
				t.Fatalf("prepared.kinds = %#v, want empty", prepared.kinds)
			}
			if len(prepared.children) != 0 {
				t.Fatalf("prepared.children len = %d, want 0", len(prepared.children))
			}
		})
	}
}

func TestManagedRuntimeKindsFromReaderAcceptsLark(t *testing.T) {
	v := viper.New()
	v.Set("console.managed_runtimes", []string{"telegram", "lark", "slack", "lark"})

	got, err := managedRuntimeKindsFromReader(v)
	if err != nil {
		t.Fatalf("managedRuntimeKindsFromReader() error = %v, want nil", err)
	}
	want := []string{"telegram", "lark", "slack"}
	if len(got) != len(want) {
		t.Fatalf("managedRuntimeKindsFromReader() = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("managedRuntimeKindsFromReader() = %#v, want %#v", got, want)
		}
	}
}

func TestManagedRuntimeKindsFromReaderRejectsUnsupportedValue(t *testing.T) {
	v := viper.New()
	v.Set("console.managed_runtimes", []string{"telegram", "line"})

	_, err := managedRuntimeKindsFromReader(v)
	if err == nil || err.Error() == "" {
		t.Fatalf("managedRuntimeKindsFromReader() error = %v, want unsupported value", err)
	}
}

func TestManagedRuntimeSupervisorRejectsMissingConfigReader(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	supervisor := newManagedRuntimeSupervisor(nil, false, false)
	if _, err := supervisor.PrepareReload(nil); err == nil {
		t.Fatal("PrepareReload(nil) error = nil, want missing config reader")
	}
}
