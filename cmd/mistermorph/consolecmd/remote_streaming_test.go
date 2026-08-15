package consolecmd

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestConsoleTaskStreamProxiesRemoteConsoleEndpoint(t *testing.T) {
	const (
		endpointRef  = "ep_remote"
		taskID       = "task_remote"
		runtimeToken = "runtime-token"
	)

	remote, remoteRuntime := newConsoleRuntimeMountTestServer("/morph", runtimeToken)
	remoteRuntime.streamHub = newConsoleStreamHub()
	remoteRuntime.streamHub.PublishReasoning(taskID, "remote reasoning")
	remoteHTTP := httptest.NewServer(remote.handler())
	remoteRuntimeURL := remoteHTTP.URL + "/morph/runtime"

	unauthorizedURL := "ws" + remoteHTTP.URL[len("http"):] + "/morph/runtime/stream/ws?task_id=" + taskID
	unauthorized, resp, err := websocket.DefaultDialer.Dial(unauthorizedURL, nil)
	if unauthorized != nil {
		_ = unauthorized.Close()
		t.Fatal("runtime stream accepted a connection without a bearer token")
	}
	if err == nil || resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("runtime stream without token = err %v, response %#v", err, resp)
	}
	_ = resp.Body.Close()

	console, localRuntime := newConsoleRuntimeMountTestServer("/", "")
	localRuntime.streamHub = newConsoleStreamHub()
	console.streamTickets = newSessionStore("")
	remoteEndpoint := runtimeEndpoint{
		Ref:    endpointRef,
		Name:   "Remote",
		URL:    remoteRuntimeURL,
		Client: newDaemonTaskClient(remoteRuntimeURL, runtimeToken),
	}
	console.endpoints = []runtimeEndpoint{remoteEndpoint}
	console.endpointByRef = map[string]runtimeEndpoint{endpointRef: remoteEndpoint}
	consoleHTTP := httptest.NewServer(console.handler())

	t.Cleanup(func() {
		console.webSockets.CloseAndWait()
		consoleHTTP.Close()
		remote.webSockets.CloseAndWait()
		remoteHTTP.Close()
	})

	ticket, _, err := console.streamTickets.Create(time.Minute)
	if err != nil {
		t.Fatalf("create stream ticket: %v", err)
	}
	streamURL := "ws" + consoleHTTP.URL[len("http"):] + "/api/stream/ws?" + url.Values{
		"endpoint": {endpointRef},
		"task_id":  {taskID},
		"ticket":   {ticket},
	}.Encode()
	header := http.Header{"Origin": []string{consoleHTTP.URL}}
	conn, _, err := websocket.DefaultDialer.Dial(streamURL, header)
	if err != nil {
		t.Fatalf("dial console task stream: %v", err)
	}
	defer conn.Close()
	if err := conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatalf("set read deadline: %v", err)
	}

	var frame consoleStreamFrame
	if err := conn.ReadJSON(&frame); err != nil {
		t.Fatalf("read remote stream frame: %v", err)
	}
	if frame.TaskID != taskID || frame.Reasoning != "remote reasoning" {
		t.Fatalf("remote stream frame = %#v", frame)
	}
}
