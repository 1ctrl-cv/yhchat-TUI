package api

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha1"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/textproto"
	"net/url"
	"strings"
	"sync"
	"time"

	"yhchat-tui/internal/logging"
)

// wsGUID is the RFC 6455 handshake magic string.
const wsGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

// WebSocket opcodes.
const (
	wsOpContinuation byte = 0x0
	wsOpText         byte = 0x1
	wsOpBinary       byte = 0x2
	wsOpClose        byte = 0x8
	wsOpPing         byte = 0x9
	wsOpPong         byte = 0xA
)

// ErrWSClosed reports a closed WebSocket connection.
var ErrWSClosed = errors.New("WebSocket 连接已关闭")

// WSEventKind identifies a WebSocket event.
type WSEventKind string

// WebSocket event kinds.
const (
	WSEventConnected    WSEventKind = "connected"
	WSEventDisconnected WSEventKind = "disconnected"
	WSEventPush         WSEventKind = "push_message"
	WSEventEdit         WSEventKind = "edit_message"
	WSEventBlocked      WSEventKind = "blocked_message"
	WSEventRecall       WSEventKind = "recall_message"
	WSEventStream       WSEventKind = "stream_message"
	WSEventInviteApply  WSEventKind = "invite_apply"
	WSEventHeartbeatAck WSEventKind = "heartbeat_ack"
	WSEventDraft        WSEventKind = "draft_input"
	WSEventUnknown      WSEventKind = "unknown"
)

// WSEvent is one decoded realtime event.
type WSEvent struct {
	Kind   WSEventKind
	Cmd    string
	Push   *PushMessage
	Recall *RecallMessage
	Stream *StreamMessage
	Err    error

	// Raw is the Any.value payload for events the chat layer decodes itself
	// (currently draft_input).
	Raw []byte
}

// WSLoginInfo is the payload of the WebSocket "login" command.
type WSLoginInfo struct {
	UserID   string
	Token    string
	Platform string
	DeviceID string
}

// WSURLFromBase derives the realtime endpoint from the HTTP API base URL.
func WSURLFromBase(baseURL string) string {
	u, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || u.Host == "" {
		return "wss://chat-ws-go.jwzhd.com/ws"
	}
	switch u.Hostname() {
	case "chat-go.jwzhd.com", "chat.jwzhd.com", "www.yhchat.com":
		return "wss://chat-ws-go.jwzhd.com/ws"
	}
	scheme := "ws"
	if u.Scheme == "https" || u.Scheme == "wss" {
		scheme = "wss"
	}
	return scheme + "://" + u.Host + "/ws"
}

// WSClient maintains the realtime connection: it logs in, keeps the heartbeat
// running and reconnects with backoff. Events are published on a channel.
type WSClient struct {
	URL   string
	Login WSLoginInfo

	events chan WSEvent
	done   chan struct{}
	once   sync.Once
	wg     sync.WaitGroup

	mu   sync.Mutex
	conn *WSConn

	// hbInterval is the heartbeat period (overridable in tests).
	hbInterval time.Duration
}

// NewWSClient creates a realtime client for the given HTTP API base URL.
func NewWSClient(baseURL string, login WSLoginInfo) *WSClient {
	return &WSClient{
		URL:        WSURLFromBase(baseURL),
		Login:      login,
		events:     make(chan WSEvent, 128),
		done:       make(chan struct{}),
		hbInterval: 25 * time.Second,
	}
}

// Events returns the event channel. It is closed when the client is shut down.
func (c *WSClient) Events() <-chan WSEvent { return c.events }

// Start launches the connection loop.
func (c *WSClient) Start() {
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		defer close(c.events)
		c.run()
	}()
}

// Close shuts the client down and interrupts any active connection.
func (c *WSClient) Close() {
	c.once.Do(func() {
		close(c.done)
		c.mu.Lock()
		conn := c.conn
		c.mu.Unlock()
		if conn != nil {
			_ = conn.Close()
		}
	})
}

// run is the reconnect loop.
func (c *WSClient) run() {
	backoff := time.Second
	for {
		if c.stopped() {
			return
		}
		err := c.session()
		if c.stopped() {
			return
		}
		c.emit(WSEvent{Kind: WSEventDisconnected, Err: err})
		select {
		case <-c.done:
			return
		case <-time.After(backoff):
		}
		if backoff < 30*time.Second {
			backoff *= 2
			if backoff > 30*time.Second {
				backoff = 30 * time.Second
			}
		}
	}
}

// stopped reports whether Close was called.
func (c *WSClient) stopped() bool {
	select {
	case <-c.done:
		return true
	default:
		return false
	}
}

// emit publishes an event without blocking the connection loop.
func (c *WSClient) emit(ev WSEvent) {
	select {
	case c.events <- ev:
	case <-c.done:
	default:
		// Consumer is slow; drop the event rather than stalling the reader.
	}
}

// session dials once, logs in, keeps the heartbeat running and reads events.
func (c *WSClient) session() error {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	conn, err := DialWS(ctx, c.URL)
	if err != nil {
		return err
	}
	c.mu.Lock()
	c.conn = conn
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		if c.conn == conn {
			c.conn = nil
		}
		c.mu.Unlock()
		_ = conn.Close()
	}()

	loginPayload := map[string]any{
		"seq": randomWSToken(),
		"cmd": "login",
		"data": map[string]string{
			"userId":   c.Login.UserID,
			"token":    c.Login.Token,
			"platform": c.Login.Platform,
			"deviceId": c.Login.DeviceID,
		},
	}
	if err := conn.WriteJSON(loginPayload); err != nil {
		return fmt.Errorf("WebSocket 登录失败: %w", err)
	}
	logging.Info("WebSocket 已连接: %s", c.URL)
	c.emit(WSEvent{Kind: WSEventConnected})

	// Heartbeat loop.
	hbStop := make(chan struct{})
	var hbWG sync.WaitGroup
	hbWG.Add(1)
	go func() {
		defer hbWG.Done()
		interval := c.hbInterval
		if interval <= 0 {
			interval = 25 * time.Second
		}
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-hbStop:
				return
			case <-c.done:
				return
			case <-ticker.C:
				_ = conn.WriteJSON(map[string]any{
					"seq":  randomWSToken(),
					"cmd":  "heartbeat",
					"data": map[string]any{},
				})
			}
		}
	}()
	defer func() {
		close(hbStop)
		hbWG.Wait()
	}()

	// Read loop.
	for {
		_ = conn.SetReadDeadline(time.Now().Add(90 * time.Second))
		opcode, payload, readErr := conn.ReadMessage()
		if readErr != nil {
			logging.Info("WebSocket 连接结束: %v", readErr)
			return readErr
		}
		if opcode == wsOpText {
			// Text frames are protocol errors from the server; ignore them.
			continue
		}
		event := decodeWSEvent(payload)
		logging.Debug("WebSocket 事件 cmd=%q kind=%s", event.Cmd, event.Kind)
		select {
		case c.events <- event:
		case <-c.done:
			return nil
		}
	}
}

// decodeWSEvent decodes one WsWrapper binary frame into an event.
func decodeWSEvent(payload []byte) WSEvent {
	wrapper, err := decodeWSWrapper(payload)
	if err != nil {
		return WSEvent{Kind: WSEventUnknown, Err: err}
	}
	cmd := strings.ToLower(wrapper.Cmd)
	switch {
	case cmd == "push_message":
		if push, pushErr := decodePushMessage(wrapper.Value); pushErr == nil {
			if push.DeleteTimestamp > 0 {
				return WSEvent{Kind: WSEventRecall, Cmd: wrapper.Cmd,
					Recall: &RecallMessage{ChatID: push.ChatID, ChatType: push.ChatType,
						MsgIDs: []string{push.MsgID}, MsgSeq: push.MsgSeq}}
			}
			return WSEvent{Kind: WSEventPush, Cmd: wrapper.Cmd, Push: push}
		}
		return WSEvent{Kind: WSEventUnknown, Cmd: wrapper.Cmd, Err: errWSDecode}
	case cmd == "edit_message":
		if push, pushErr := decodePushMessage(wrapper.Value); pushErr == nil {
			return WSEvent{Kind: WSEventEdit, Cmd: wrapper.Cmd, Push: push}
		}
		return WSEvent{Kind: WSEventUnknown, Cmd: wrapper.Cmd, Err: errWSDecode}
	case cmd == "blocked_message":
		if push, pushErr := decodePushMessage(wrapper.Value); pushErr == nil {
			return WSEvent{Kind: WSEventBlocked, Cmd: wrapper.Cmd, Push: push}
		}
		return WSEvent{Kind: WSEventUnknown, Cmd: wrapper.Cmd, Err: errWSDecode}
	case cmd == "stream_message":
		if stream, streamErr := decodeStreamMessage(wrapper.Value); streamErr == nil {
			return WSEvent{Kind: WSEventStream, Cmd: wrapper.Cmd, Stream: stream}
		}
		return WSEvent{Kind: WSEventUnknown, Cmd: wrapper.Cmd, Err: errWSDecode}
	case strings.Contains(cmd, "recall") || strings.Contains(cmd, "revoke"):
		// Prefer the sparse PushMessage shape when it carries a usable chat id.
		if push, pushErr := decodePushMessage(wrapper.Value); pushErr == nil && push.ChatID != "" && push.MsgID != "" {
			return WSEvent{Kind: WSEventRecall, Cmd: wrapper.Cmd,
				Recall: &RecallMessage{ChatID: push.ChatID, ChatType: push.ChatType,
					MsgIDs: []string{push.MsgID}, MsgSeq: push.MsgSeq}}
		}
		if recall, recallErr := decodeRecallMessage(wrapper.Value); recallErr == nil {
			return WSEvent{Kind: WSEventRecall, Cmd: wrapper.Cmd, Recall: recall}
		}
		return WSEvent{Kind: WSEventUnknown, Cmd: wrapper.Cmd, Err: errWSDecode}
	case cmd == "heartbeat_ack":
		return WSEvent{Kind: WSEventHeartbeatAck, Cmd: wrapper.Cmd}
	case cmd == "invite_apply":
		return WSEvent{Kind: WSEventInviteApply, Cmd: wrapper.Cmd}
	case cmd == "draft_input":
		return WSEvent{Kind: WSEventDraft, Cmd: wrapper.Cmd, Raw: wrapper.Value}
	default:
		return WSEvent{Kind: WSEventUnknown, Cmd: wrapper.Cmd}
	}
}

// randomWSToken returns a random hex string used as the seq value.
func randomWSToken() string {
	var buf [12]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf[:])
}

// ---------------------------------------------------------------------------
// Minimal RFC 6455 client

// WSConn is a client-side WebSocket connection.
type WSConn struct {
	conn    net.Conn
	br      *bufio.Reader
	writeMu sync.Mutex
}

// DialWS performs the HTTP Upgrade handshake and returns a WebSocket client.
func DialWS(ctx context.Context, rawURL string) (*WSConn, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("WebSocket 地址无效: %w", err)
	}
	if u.Host == "" {
		return nil, errors.New("WebSocket 地址缺少主机名")
	}
	host := u.Host
	switch u.Scheme {
	case "wss":
		if !strings.Contains(host, ":") {
			host += ":443"
		}
	case "ws":
		if !strings.Contains(host, ":") {
			host += ":80"
		}
	default:
		return nil, fmt.Errorf("不支持的 WebSocket 协议: %s", u.Scheme)
	}

	dialer := &net.Dialer{Timeout: 15 * time.Second}
	var conn net.Conn
	if u.Scheme == "wss" {
		tlsDialer := &tls.Dialer{
			NetDialer: dialer,
			Config:    &tls.Config{ServerName: u.Hostname(), MinVersion: tls.VersionTLS12},
		}
		conn, err = tlsDialer.DialContext(ctx, "tcp", host)
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", host)
	}
	if err != nil {
		return nil, fmt.Errorf("连接 WebSocket 失败: %w", err)
	}

	keyBytes := make([]byte, 16)
	if _, err := rand.Read(keyBytes); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("生成 WebSocket key 失败: %w", err)
	}
	key := base64.StdEncoding.EncodeToString(keyBytes)

	path := u.RequestURI()
	if path == "" {
		path = "/"
	}
	request := fmt.Sprintf("GET %s HTTP/1.1\r\nHost: %s\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n"+
		"Sec-WebSocket-Key: %s\r\nSec-WebSocket-Version: 13\r\nUser-Agent: %s\r\n\r\n",
		path, u.Host, key, userAgent)
	_ = conn.SetWriteDeadline(time.Now().Add(15 * time.Second))
	if _, err := io.WriteString(conn, request); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("发送 WebSocket 握手失败: %w", err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(15 * time.Second))

	br := bufio.NewReader(conn)
	tp := textproto.NewReader(br)
	statusLine, err := tp.ReadLine()
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("读取 WebSocket 握手响应失败: %w", err)
	}
	fields := strings.Fields(statusLine)
	if len(fields) < 2 || fields[1] != "101" {
		_ = conn.Close()
		return nil, fmt.Errorf("WebSocket 握手被拒绝: %s", statusLine)
	}
	headers, err := tp.ReadMIMEHeader()
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("读取 WebSocket 响应头失败: %w", err)
	}
	expected := base64.StdEncoding.EncodeToString(sha1Digest(key + wsGUID))
	if got := headers.Get("Sec-WebSocket-Accept"); got != expected {
		_ = conn.Close()
		return nil, errors.New("WebSocket 握手校验失败")
	}
	_ = conn.SetReadDeadline(time.Time{})
	_ = conn.SetWriteDeadline(time.Time{})
	return &WSConn{conn: conn, br: br}, nil
}

// sha1Digest returns the SHA-1 digest of s.
func sha1Digest(s string) []byte {
	sum := sha1.Sum([]byte(s))
	return sum[:]
}

// WriteJSON sends v as a masked text frame.
func (c *WSConn) WriteJSON(v any) error {
	payload, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return c.WriteMessage(wsOpText, payload)
}

// WriteMessage sends one masked frame.
func (c *WSConn) WriteMessage(opcode byte, payload []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return writeWSFrame(c.conn, opcode, payload)
}

// ReadMessage reads the next text/binary message, handling ping/pong and
// fragmented frames.
func (c *WSConn) ReadMessage() (byte, []byte, error) {
	var messageOpcode byte
	var payload []byte
	for {
		opcode, fin, data, err := readWSFrame(c.br)
		if err != nil {
			return 0, nil, err
		}
		switch opcode {
		case wsOpPing:
			if err := c.WriteMessage(wsOpPong, data); err != nil {
				return 0, nil, err
			}
			continue
		case wsOpPong:
			continue
		case wsOpClose:
			_ = c.WriteMessage(wsOpClose, data)
			return 0, nil, ErrWSClosed
		case wsOpContinuation:
			payload = append(payload, data...)
			if fin {
				return messageOpcode, payload, nil
			}
		case wsOpText, wsOpBinary:
			messageOpcode = opcode
			payload = append(payload[:0], data...)
			if fin {
				return messageOpcode, payload, nil
			}
		default:
			return 0, nil, fmt.Errorf("未知的 WebSocket 帧类型: %d", opcode)
		}
	}
}

// Close closes the underlying connection.
func (c *WSConn) Close() error { return c.conn.Close() }

// SetReadDeadline forwards to the underlying connection.
func (c *WSConn) SetReadDeadline(t time.Time) error { return c.conn.SetReadDeadline(t) }

// writeWSFrame writes a masked client frame.
func writeWSFrame(w io.Writer, opcode byte, payload []byte) error {
	header := make([]byte, 0, 14)
	header = append(header, 0x80|opcode) // FIN + opcode
	length := len(payload)
	switch {
	case length < 126:
		header = append(header, byte(length)|0x80) // mask bit
	case length <= 0xFFFF:
		header = append(header, 126|0x80, byte(length>>8), byte(length))
	default:
		header = append(header, 127|0x80)
		for shift := 56; shift >= 0; shift -= 8 {
			header = append(header, byte(uint64(length)>>uint(shift)))
		}
	}
	mask := make([]byte, 4)
	if _, err := rand.Read(mask); err != nil {
		return err
	}
	header = append(header, mask...)
	masked := make([]byte, length)
	for i := range payload {
		masked[i] = payload[i] ^ mask[i%4]
	}
	if _, err := w.Write(header); err != nil {
		return err
	}
	_, err := w.Write(masked)
	return err
}

// readWSFrame reads one frame. It returns whether the frame was final.
func readWSFrame(r *bufio.Reader) (opcode byte, fin bool, payload []byte, err error) {
	var head [2]byte
	if _, err = io.ReadFull(r, head[:]); err != nil {
		return 0, false, nil, err
	}
	fin = head[0]&0x80 != 0
	opcode = head[0] & 0x0F
	masked := head[1]&0x80 != 0
	length := uint64(head[1] & 0x7F)
	switch length {
	case 126:
		var ext [2]byte
		if _, err = io.ReadFull(r, ext[:]); err != nil {
			return 0, false, nil, err
		}
		length = uint64(ext[0])<<8 | uint64(ext[1])
	case 127:
		var ext [8]byte
		if _, err = io.ReadFull(r, ext[:]); err != nil {
			return 0, false, nil, err
		}
		length = 0
		for _, b := range ext {
			length = length<<8 | uint64(b)
		}
	}
	if length > maxBodyBytes {
		return 0, false, nil, errors.New("WebSocket 帧过大")
	}
	var mask [4]byte
	if masked {
		if _, err = io.ReadFull(r, mask[:]); err != nil {
			return 0, false, nil, err
		}
	}
	payload = make([]byte, length)
	if _, err = io.ReadFull(r, payload); err != nil {
		return 0, false, nil, err
	}
	if masked {
		for i := range payload {
			payload[i] ^= mask[i%4]
		}
	}
	return opcode, fin, payload, nil
}
