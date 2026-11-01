package mcpserver

import (
	"context"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestSubscriptionsKeepToTheirResources(t *testing.T) {
	e := &env{caller: Caller{Kind: "token", ID: "t", Permissions: map[string]bool{"assets.read": true}},
		api: func(context.Context, string, string, []byte, string) Response {
			return Response{Status: 200, Body: []byte(`{"events":[]}`)}
		}}
	sub := func(uri string) error {
		return e.subscribe(nil)(context.Background(), &mcp.SubscribeRequest{Params: &mcp.SubscribeParams{URI: uri}})
	}
	if err := sub("bam://somewhere"); err == nil {
		t.Error("subscribed to something else")
	}
	if err := sub("bam://events"); err == nil {
		t.Error("subscribed to events without events.read")
	}
	q, err := eventsQuery("bam://events?kinds=asset.hook&hooks=identify&other=x")
	if err != nil || q.Encode() != "hooks=identify&kinds=asset.hook" {
		t.Errorf("query: %v %v", q, err)
	}
	if _, err := eventsQuery("bam://eventsplus"); err == nil {
		t.Error("bam://eventsplus is events")
	}
}
