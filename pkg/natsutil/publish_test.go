package natsutil

import (
	"encoding/json"
	"math"
	"testing"

	natsserver "github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
)

func startTestServer(t *testing.T) (*natsserver.Server, *nats.Conn) {
	t.Helper()
	opts := &natsserver.Options{
		Host: "127.0.0.1",
		Port: -1,
	}
	ns, err := natsserver.NewServer(opts)
	if err != nil {
		t.Fatalf("start nats server: %v", err)
	}
	ns.Start()
	if !ns.ReadyForConnections(2e9) {
		t.Fatal("nats server not ready")
	}

	nc, err := nats.Connect(ns.ClientURL())
	if err != nil {
		ns.Shutdown()
		t.Fatalf("connect to test server: %v", err)
	}
	t.Cleanup(func() {
		nc.Close()
		ns.Shutdown()
	})
	return ns, nc
}

func TestPublish_NilConnection(t *testing.T) {
	err := Publish(nil, "test.subject", []byte("hello"))
	if err == nil {
		t.Fatal("expected error for nil connection, got nil")
	}
}

func TestPublishJSON_NilConnection(t *testing.T) {
	err := PublishJSON(nil, "test.subject", map[string]string{"key": "value"})
	if err == nil {
		t.Fatal("expected error for nil connection, got nil")
	}
}

func TestPublish_Success(t *testing.T) {
	_, nc := startTestServer(t)

	sub, err := nc.SubscribeSync("test.pub")
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	payload := []byte("hello world")
	if err := Publish(nc, "test.pub", payload); err != nil {
		t.Fatalf("Publish returned error: %v", err)
	}

	msg, err := sub.NextMsg(2e9)
	if err != nil {
		t.Fatalf("did not receive message: %v", err)
	}
	if string(msg.Data) != "hello world" {
		t.Fatalf("unexpected data: %q", msg.Data)
	}
}

func TestPublishJSON_Success(t *testing.T) {
	_, nc := startTestServer(t)

	sub, err := nc.SubscribeSync("test.json")
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	input := map[string]string{"key": "value"}
	if err := PublishJSON(nc, "test.json", input); err != nil {
		t.Fatalf("PublishJSON returned error: %v", err)
	}

	msg, err := sub.NextMsg(2e9)
	if err != nil {
		t.Fatalf("did not receive message: %v", err)
	}

	var got map[string]string
	if err := json.Unmarshal(msg.Data, &got); err != nil {
		t.Fatalf("unmarshal received message: %v", err)
	}
	if got["key"] != "value" {
		t.Fatalf("unexpected payload: %v", got)
	}
}

func TestPublishJSON_MarshalError(t *testing.T) {
	_, nc := startTestServer(t)

	err := PublishJSON(nc, "test.badjson", math.NaN())
	if err == nil {
		t.Fatal("expected marshal error, got nil")
	}
}
