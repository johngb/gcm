package gcm

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

var (
	testMessage = Message{
		RegistrationIDs: []string{"hoiearsyt", "ienha98st"},
	}
	testIDs = []string{"hoiearsyt", "ienha98st"}
)

type testResponse struct {
	StatusCode int
	Response   *Response
}

func startTestServer(t *testing.T, responses ...*testResponse) *httptest.Server {
	i := 0
	handler := func(w http.ResponseWriter, r *http.Request) {
		if i >= len(responses) {
			t.Fatalf("server received %d requests, expected %d", i+1, len(responses))
		}
		resp := responses[i]
		status := resp.StatusCode
		if status == 0 || status == http.StatusOK {
			w.Header().Set("Content-Type", "application/json")
			respBytes, _ := json.Marshal(resp.Response)
			fmt.Fprint(w, string(respBytes))
		} else {
			w.WriteHeader(status)
		}
		i++
	}
	server := httptest.NewServer(http.HandlerFunc(handler))
	gcmSendEndpoint = server.URL
	return server
}

func TestSendInvalidAPIKey(t *testing.T) {
	server := startTestServer(t)
	defer server.Close()
	sender := &sender{APIKey: ""}
	if _, err := sender.Send(&Message{}, []string{"hoiearsyt", "ienha98st"}, 2); err == nil {
		t.Fatal("test should fail when sender's APIKey is \"\"")
	}
}

func TestSendInvalidMessage(t *testing.T) {
	server := startTestServer(t)
	defer server.Close()
	sender := &sender{APIKey: "test"}
	if _, err := sender.Send(nil, testIDs, 1); err == nil {
		t.Fatal("test should fail when message is nil")
	}
	if _, err := sender.Send(&Message{}, []string{}, 1); err == nil {
		t.Fatal("test should fail when message RegistrationIDs field is an empty slice")
	}
	if _, err := sender.Send(&Message{}, make([]string, 1001), 1); err == nil {
		t.Fatal("test should fail when more than 1000 RegistrationIDs are specified")
	}
	if _, err := sender.Send(&Message{TimeToLive: -1}, testIDs, 1); err == nil {
		t.Fatal("test should fail when message TimeToLive field is negative")
	}
	if _, err := sender.Send(&Message{TimeToLive: 2419201}, testIDs, 1); err == nil {
		t.Fatal("test should fail when message TimeToLive field is greater than 2419200")
	}
}

// func TestSendNoRetrySuccess(t *testing.T) {
// 	server := startTestServer(t, &testResponse{Response: &Response{}})
// 	defer server.Close()
// 	sender := &sender{APIKey: "test"}
// 	msg := NewMessage(`{"key": "value"}`, "1")
// 	if _, err := sender.sendNoRetry(msg); err.Err != nil {
// 		t.Fatalf("test failed with error: %s", err.Err)
// 	}
// }

// func TestSendNoRetryNonrecoverableFailure(t *testing.T) {
// 	server := startTestServer(t, &testResponse{StatusCode: http.StatusBadRequest})
// 	defer server.Close()
// 	sender := &sender{APIKey: "test"}
// 	msg := NewMessage(`{"key": "value"}`, "1")
// 	if _, err := sender.sendNoRetry(msg); err == nil {
// 		t.Fatal("test expected non-recoverable error")
// 	}
// }

// func TestSendOneRetrySuccess(t *testing.T) {
// 	server := startTestServer(t,
// 		&testResponse{Response: &Response{Failure: 1, Results: []Result{{Error: "Unavailable"}}}},
// 		&testResponse{Response: &Response{Success: 1, Results: []Result{{MessageID: "id"}}}},
// 	)
// 	defer server.Close()
// 	sender := &sender{APIKey: "test"}
// 	msg := NewMessage(`{"key": "value"}`, "1")
// 	if _, err := sender.Send(msg, 1); err.Err != nil {
// 		t.Fatal("send should succeed after one retry")
// 	}
// }

// func TestSendOneRetryFailure(t *testing.T) {
// 	server := startTestServer(t,
// 		&testResponse{Response: &Response{Failure: 1, Results: []Result{{Error: "Unavailable"}}}},
// 		&testResponse{Response: &Response{Failure: 1, Results: []Result{{Error: "Unavailable"}}}},
// 	)
// 	defer server.Close()
// 	sender := &sender{APIKey: "test"}
// 	msg := NewMessage(`{"key": "value"}`, "1")
// 	resp, err := sender.Send(msg, 1)
// 	if err.Err != nil || resp.Failure != 1 {
// 		t.Fatal("send should return response with one failure")
// 	}
// }

// func TestSendOneRetryNonrecoverableFailure(t *testing.T) {
// 	server := startTestServer(t,
// 		&testResponse{Response: &Response{Failure: 1, Results: []Result{{Error: "Unavailable"}}}},
// 		&testResponse{StatusCode: http.StatusBadRequest},
// 	)
// 	defer server.Close()
// 	sender := &sender{APIKey: "test"}
// 	msg := NewMessage(`{"key": "value"}`, "1")
// 	if _, err := sender.Send(msg, 1); err == nil {
// 		t.Fatal("send should fail after one retry")
// 	}
// }
