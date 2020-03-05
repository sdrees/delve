package dap

import (
	"flag"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/go-delve/delve/pkg/logflags"
	protest "github.com/go-delve/delve/pkg/proc/test"
	"github.com/go-delve/delve/service"
	"github.com/go-delve/delve/service/dap/daptest"
	"github.com/google/go-dap"
)

const stopOnEntry bool = true

func TestMain(m *testing.M) {
	var logOutput string
	flag.StringVar(&logOutput, "log-output", "", "configures log output")
	flag.Parse()
	logflags.Setup(logOutput != "", logOutput, "")
	os.Exit(protest.RunTestsWithFixtures(m))
}

// name is for _fixtures/<name>.go
func runTest(t *testing.T, name string, test func(c *daptest.Client, f protest.Fixture)) {
	var buildFlags protest.BuildFlags
	fixture := protest.BuildFixture(name, buildFlags)

	// Start the DAP server.
	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatal(err)
	}
	disconnectChan := make(chan struct{})
	server := NewServer(&service.Config{
		Listener:       listener,
		Backend:        "default",
		DisconnectChan: disconnectChan,
	})
	server.Run()
	// Give server time to start listening for clients
	time.Sleep(100 * time.Millisecond)

	var stopOnce sync.Once
	// Run a goroutine that stops the server when disconnectChan is signaled.
	// This helps us test that certain events cause the server to stop as
	// expected.
	go func() {
		<-disconnectChan
		stopOnce.Do(func() { server.Stop() })
	}()

	client := daptest.NewClient(listener.Addr().String())
	defer client.Close()

	defer func() {
		stopOnce.Do(func() { server.Stop() })
	}()

	test(client, fixture)
}

func TestStopOnEntry(t *testing.T) {
	runTest(t, "increment", func(client *daptest.Client, fixture protest.Fixture) {
		// This test exhaustively tests Seq and RequestSeq on all messages from the
		// server. Other tests shouldn't necessarily repeat these checks.
		client.InitializeRequest()
		initResp := client.ExpectInitializeResponse(t)
		if initResp.Seq != 0 || initResp.RequestSeq != 0 {
			t.Errorf("got %#v, want Seq=0, RequestSeq=0", initResp)
		}

		client.LaunchRequest("exec", fixture.Path, stopOnEntry)
		initEv := client.ExpectInitializedEvent(t)
		if initEv.Seq != 0 {
			t.Errorf("got %#v, want Seq=0", initEv)
		}

		launchResp := client.ExpectLaunchResponse(t)
		if launchResp.Seq != 0 || launchResp.RequestSeq != 1 {
			t.Errorf("got %#v, want Seq=0, RequestSeq=1", launchResp)
		}

		client.SetExceptionBreakpointsRequest()
		sResp := client.ExpectSetExceptionBreakpointsResponse(t)
		if sResp.Seq != 0 || sResp.RequestSeq != 2 {
			t.Errorf("got %#v, want Seq=0, RequestSeq=2", sResp)
		}

		client.ConfigurationDoneRequest()
		stopEvent := client.ExpectStoppedEvent(t)
		if stopEvent.Seq != 0 ||
			stopEvent.Body.Reason != "breakpoint" ||
			stopEvent.Body.ThreadId != 1 ||
			!stopEvent.Body.AllThreadsStopped {
			t.Errorf("got %#v, want Seq=0, Body={Reason=\"breakpoint\", ThreadId=1, AllThreadsStopped=true}", stopEvent)
		}

		cdResp := client.ExpectConfigurationDoneResponse(t)
		if cdResp.Seq != 0 || cdResp.RequestSeq != 3 {
			t.Errorf("got %#v, want Seq=0, RequestSeq=3", cdResp)
		}

		client.ContinueRequest(1)
		contResp := client.ExpectContinueResponse(t)
		if contResp.Seq != 0 || contResp.RequestSeq != 4 {
			t.Errorf("got %#v, want Seq=0, RequestSeq=4", contResp)
		}

		termEv := client.ExpectTerminatedEvent(t)
		if termEv.Seq != 0 {
			t.Errorf("got %#v, want Seq=0", termEv)
		}

		client.DisconnectRequest()
		dResp := client.ExpectDisconnectResponse(t)
		if dResp.Seq != 0 || dResp.RequestSeq != 5 {
			t.Errorf("got %#v, want Seq=0, RequestSeq=5", dResp)
		}
	})
}

func TestSetBreakpoint(t *testing.T) {
	runTest(t, "increment", func(client *daptest.Client, fixture protest.Fixture) {
		client.InitializeRequest()
		client.ExpectInitializeResponse(t)

		client.LaunchRequest("exec", fixture.Path, !stopOnEntry)
		client.ExpectInitializedEvent(t)
		launchResp := client.ExpectLaunchResponse(t)
		if launchResp.RequestSeq != 1 {
			t.Errorf("got %#v, want RequestSeq=1", launchResp)
		}

		client.SetBreakpointsRequest(fixture.Source, []int{8, 100})
		sResp := client.ExpectSetBreakpointsResponse(t)
		if len(sResp.Body.Breakpoints) != 1 {
			t.Errorf("got %#v, want len(Breakpoints)=1", sResp)
		}
		bkpt0 := sResp.Body.Breakpoints[0]
		if !bkpt0.Verified || bkpt0.Line != 8 {
			t.Errorf("got breakpoints[0] = %#v, want Verified=true, Line=8", bkpt0)
		}

		client.SetExceptionBreakpointsRequest()
		client.ExpectSetExceptionBreakpointsResponse(t)

		client.ConfigurationDoneRequest()
		cdResp := client.ExpectConfigurationDoneResponse(t)
		if cdResp.RequestSeq != 4 {
			t.Errorf("got %#v, want RequestSeq=4", cdResp)
		}

		client.ContinueRequest(1)
		stopEvent1 := client.ExpectStoppedEvent(t)
		if stopEvent1.Body.Reason != "breakpoint" ||
			stopEvent1.Body.ThreadId != 1 ||
			!stopEvent1.Body.AllThreadsStopped {
			t.Errorf("got %#v, want Body={Reason=\"breakpoint\", ThreadId=1, AllThreadsStopped=true}", stopEvent1)
		}
		client.ExpectContinueResponse(t)

		client.ContinueRequest(1)
		client.ExpectTerminatedEvent(t)
		client.ExpectContinueResponse(t)

		client.DisconnectRequest()
		client.ExpectDisconnectResponse(t)
	})
}

// runDebugSesion is a helper for executing the standard init and shutdown
// sequences while specifying unique launch criteria via parameters.
func runDebugSession(t *testing.T, client *daptest.Client, launchRequest func()) {
	client.InitializeRequest()
	client.ExpectInitializeResponse(t)

	launchRequest()
	client.ExpectInitializedEvent(t)
	client.ExpectLaunchResponse(t)

	client.ConfigurationDoneRequest()
	client.ExpectConfigurationDoneResponse(t)

	client.ExpectTerminatedEvent(t)
	client.DisconnectRequest()
	client.ExpectDisconnectResponse(t)
}

func TestLaunchDebugRequest(t *testing.T) {
	runTest(t, "increment", func(client *daptest.Client, fixture protest.Fixture) {
		// We reuse the harness that builds, but ignore the actual binary.
		runDebugSession(t, client, func() {
			// Use the default output directory.
			client.LaunchRequestWithArgs(map[string]interface{}{
				"mode": "debug", "program": fixture.Source})
		})
	})
}

func TestLaunchTestRequest(t *testing.T) {
	runTest(t, "increment", func(client *daptest.Client, fixture protest.Fixture) {
		runDebugSession(t, client, func() {
			// We reuse the harness that builds, but ignore the actual binary.
			fixtures := protest.FindFixturesDir()
			testdir, _ := filepath.Abs(filepath.Join(fixtures, "buildtest"))
			client.LaunchRequestWithArgs(map[string]interface{}{
				"mode": "test", "program": testdir, "output": "__mytestdir"})
		})
	})
}

func TestBadLaunchRequests(t *testing.T) {
	runTest(t, "increment", func(client *daptest.Client, fixture protest.Fixture) {
		seqCnt := 0
		expectFailedToLaunch := func(response *dap.ErrorResponse) {
			t.Helper()
			if response.RequestSeq != seqCnt {
				t.Errorf("RequestSeq got %d, want %d", seqCnt, response.RequestSeq)
			}
			if response.Command != "launch" {
				t.Errorf("Command got %q, want \"launch\"", response.Command)
			}
			if response.Message != "Failed to launch" {
				t.Errorf("Message got %q, want \"Failed to launch\"", response.Message)
			}
			if response.Body.Error.Id != 3000 {
				t.Errorf("Id got %d, want 3000", response.Body.Error.Id)
			}
			seqCnt++
		}

		expectFailedToLaunchWithMessage := func(response *dap.ErrorResponse, errmsg string) {
			t.Helper()
			expectFailedToLaunch(response)
			if response.Body.Error.Format != errmsg {
				t.Errorf("\ngot  %q\nwant %q", response.Body.Error.Format, errmsg)
			}
		}

		// Test for the DAP-specific detailed error message.
		client.LaunchRequest("exec", "", stopOnEntry)
		expectFailedToLaunchWithMessage(client.ExpectErrorResponse(t),
			"Failed to launch: The program attribute is missing in debug configuration.")

		client.LaunchRequestWithArgs(map[string]interface{}{"program": 12345})
		expectFailedToLaunchWithMessage(client.ExpectErrorResponse(t),
			"Failed to launch: The program attribute is missing in debug configuration.")

		client.LaunchRequestWithArgs(map[string]interface{}{"program": nil})
		expectFailedToLaunchWithMessage(client.ExpectErrorResponse(t),
			"Failed to launch: The program attribute is missing in debug configuration.")

		client.LaunchRequestWithArgs(map[string]interface{}{})
		expectFailedToLaunchWithMessage(client.ExpectErrorResponse(t),
			"Failed to launch: The program attribute is missing in debug configuration.")

		client.LaunchRequest("remote", fixture.Path, stopOnEntry)
		expectFailedToLaunchWithMessage(client.ExpectErrorResponse(t),
			"Failed to launch: Unsupported 'mode' value \"remote\" in debug configuration.")

		client.LaunchRequest("notamode", fixture.Path, stopOnEntry)
		expectFailedToLaunchWithMessage(client.ExpectErrorResponse(t),
			"Failed to launch: Unsupported 'mode' value \"notamode\" in debug configuration.")

		client.LaunchRequestWithArgs(map[string]interface{}{"mode": 12345, "program": fixture.Path})
		expectFailedToLaunchWithMessage(client.ExpectErrorResponse(t),
			"Failed to launch: Unsupported 'mode' value %!q(float64=12345) in debug configuration.")

		// Skip detailed message checks for potentially different OS-specific errors.
		client.LaunchRequest("exec", fixture.Path+"_does_not_exist", stopOnEntry)
		expectFailedToLaunch(client.ExpectErrorResponse(t))

		client.LaunchRequest("debug", fixture.Path+"_does_not_exist", stopOnEntry)
		expectFailedToLaunch(client.ExpectErrorResponse(t)) // Build error

		client.LaunchRequest("exec", fixture.Source, stopOnEntry)
		expectFailedToLaunch(client.ExpectErrorResponse(t)) // Not an executable

		// We failed to launch the program. Make sure shutdown still works.
		client.DisconnectRequest()
		dresp := client.ExpectDisconnectResponse(t)
		if dresp.RequestSeq != seqCnt {
			t.Errorf("got %#v, want RequestSeq=%d", dresp, seqCnt)
		}
	})
}

func TestBadlyFormattedMessageToServer(t *testing.T) {
	runTest(t, "increment", func(client *daptest.Client, fixture protest.Fixture) {
		// Send a badly formatted message to the server, and expect it to close the
		// connection.
		client.UnknownRequest()
		time.Sleep(100 * time.Millisecond)

		_, err := client.ReadMessage()

		if err != io.EOF {
			t.Errorf("got err=%v, want io.EOF", err)
		}
	})
}
