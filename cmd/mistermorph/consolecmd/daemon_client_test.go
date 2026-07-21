package consolecmd

import (
	"net/http"
	"testing"
)

func TestNewDaemonTaskClientDownloadTransportHasResponseHeaderTimeout(t *testing.T) {
	client := newDaemonTaskClient("http://127.0.0.1", "token")
	if client.downloadClient == nil {
		t.Fatal("download client is nil")
	}
	transport, ok := client.downloadClient.Transport.(*http.Transport)
	if !ok || transport == nil {
		t.Fatalf("download transport = %T, want *http.Transport", client.downloadClient.Transport)
	}
	if transport.ResponseHeaderTimeout <= 0 {
		t.Fatalf("response header timeout = %v, want a positive duration", transport.ResponseHeaderTimeout)
	}
	if client.downloadClient.Timeout != 0 {
		t.Fatalf("download client total timeout = %v, want 0 for streaming bodies", client.downloadClient.Timeout)
	}
}
