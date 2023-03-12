package api

import (
	"fmt"
	"testing"
	"time"

	"github.com/rumsystem/quorum/pkg/chainapi/handlers"
)

func TestGetGroupContent(t *testing.T) {
	t.Parallel()

	// create group
	createGroupParam := handlers.CreateGroupParam{
		GroupName:      "test-group-content",
		ConsensusType:  "poa",
		EncryptionType: "public",
		AppKey:         "default",
	}
	group, err := createGroup(peerapi, createGroupParam)
	if err != nil {
		t.Errorf("createGroup failed: %s, payload: %+v", err, createGroupParam)
	}

	// join group
	joinGroupParam := handlers.JoinGroupParamV2{
		Seed: group.Seed,
	}
	if _, err := joinGroup(peerapi2, joinGroupParam); err != nil {
		t.Errorf("joinGroup failed: %s, payload: %+v", err, joinGroupParam)
	}

	// post to group
	content := fmt.Sprintf("%s hello world", RandString(4))
	name := fmt.Sprintf("%s post to group testing", RandString(4))
	postGroupParam := PostGroupParam{
		GroupID: group.GroupId,
		Data: map[string]interface{}{
			"type":    "Note",
			"content": content,
			"name":    name,
		},
	}

	postResult, err := postToGroup(peerapi, postGroupParam)
	if err != nil {
		t.Errorf("postToGroup failed: %s, payload: %+v", err, postGroupParam)
	}
	if postResult.TrxId == "" {
		t.Errorf("postToGroup failed: TrxId is empty")
	}

	// FIXME
	time.Sleep(time.Second * 30)

	// check peerapi received content
	for _, api := range []string{peerapi, peerapi2} {
		receivedContent, err := isReceivedGroupContent(api, group.GroupId, postResult.TrxId)
		if err != nil {
			t.Errorf("isReceivedGroupContent failed: %s", err)
		}
		if !receivedContent {
			t.Errorf("isReceivedGroupContent failed: content not received")
		}
	}
}
