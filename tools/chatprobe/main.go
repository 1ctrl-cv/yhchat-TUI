// Temporary probe: verify Yunhu protobuf REST endpoints (conversation list,
// message list, send message) accept protobuf bodies with a bogus token.
package main

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"time"
)

const base = "https://chat-go.jwzhd.com"

func probe(name, path, contentType string, body []byte) {
	req, err := http.NewRequest(http.MethodPost, base+path, bytes.NewReader(body))
	if err != nil { panic(err) }
	if contentType != "" { req.Header.Set("Content-Type", contentType) }
	req.Header.Set("token", "bogus-token-probe")
	req.Header.Set("User-Agent", "yhchat-tui/0.1")
	c := &http.Client{Timeout: 20 * time.Second}
	resp, err := c.Do(req)
	if err != nil { fmt.Printf("%-42s ERR %v\n", name, err); return }
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	fmt.Printf("%-42s HTTP %d ct=%-28q len=%-4d hex=%x\n", name, resp.StatusCode, resp.Header.Get("Content-Type"), len(raw), raw)
}

func main() {
	// ConversationListRequest{md5=2, empty string} -> field2 empty len
	convBody := []byte{0x12, 0x00}
	for _, ct := range []string{"application/x-protobuf", "application/octet-stream", "application/protobuf", "application/json", ""} {
		probe("conversation/list ct="+ct, "/v1/conversation/list", ct, convBody)
	}
	// ListMsgRequest{msg_count=2:20, chat_type=4:1, chat_id=5:"1"}
	msgBody := []byte{0x10, 0x14, 0x20, 0x01, 0x2a, 0x01, 0x31}
	for _, ct := range []string{"application/x-protobuf", "application/octet-stream", ""} {
		probe("msg/list-message ct="+ct, "/v1/msg/list-message", ct, msgBody)
	}
	// SendMsgRequest minimal-ish: msg_id=2, chat_id=3, chat_type=4, content=5, content_type=6
	sendBody := []byte{
		0x12, 0x03, 'a', 'b', 'c',
		0x1a, 0x01, '1',
		0x20, 0x01,
		0x2a, 0x05, 0x0a, 0x03, 'h', 'i', '!',
		0x30, 0x01,
	}
	for _, ct := range []string{"application/x-protobuf", "application/octet-stream", ""} {
		probe("msg/send-message ct="+ct, "/v1/msg/send-message", ct, sendBody)
	}
}
