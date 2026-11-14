// Command mockapi is a local mock of the Yunhu (云湖) HTTP API used to try out
// the TUI without a real account:
//
//	go run ./tools/mockapi
//	go run . -server http://127.0.0.1:18080
//
// Demo accounts:
//
//	email login: demo@yunhu.chat / demo1234
//	SMS login:   any mobile number, SMS code 123456
//	image captcha: any non-empty text is accepted
//
// It is a development tool only; it never talks to the real service.
package main

import (
	"crypto/rand"
	_ "embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"google.golang.org/protobuf/encoding/protowire"
)

//go:embed captcha.png
var captchaPNG []byte

const (
	demoEmail    = "demo@yunhu.chat"
	demoPassword = "demo1234"
	demoSMSCode  = "123456"
	demoName     = "演示用户"
	demoUserID   = "100086"
)

type server struct {
	mu     sync.Mutex
	tokens map[string]string // token -> account (email or mobile)
}

func main() {
	addr := flag.String("addr", "127.0.0.1:18080", "listen address")
	flag.Parse()

	s := &server{tokens: map[string]string{}}

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/user/captcha", s.handleCaptcha)
	mux.HandleFunc("/v1/verification/get-verification-code", s.handleSendSMS)
	mux.HandleFunc("/v1/user/email-login", s.handleEmailLogin)
	mux.HandleFunc("/v1/user/verification-login", s.handleSMSLogin)
	mux.HandleFunc("/v1/user/info", s.handleUserInfo)
	mux.HandleFunc("/v1/user/logout", s.handleLogout)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		log.Printf("404 %s %s", r.Method, r.URL.Path)
		writeJSON(w, http.StatusNotFound, map[string]any{"code": -404, "msg": "not found"})
	})

	println("云湖 API mock listening on http://" + *addr)
	println("  email login : " + demoEmail + " / " + demoPassword)
	println("  SMS login   : any mobile + SMS code " + demoSMSCode)
	fmt.Fprintln(os.Stderr, "  (TUI: go run . -server http://"+*addr+")")

	if err := http.ListenAndServe(*addr, logRequests(mux)); err != nil {
		log.Fatal(err)
	}
}

// logRequests logs each request before serving it.
func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Printf("%s %s", r.Method, r.URL.Path)
		next.ServeHTTP(w, r)
	})
}

// ---------------------------------------------------------------------------
// handlers

func (s *server) handleCaptcha(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"code": -1, "msg": "method not allowed"})
		return
	}
	id := "mock-" + randomHex(8)
	dataURL := "data:image/png;base64," + base64.StdEncoding.EncodeToString(captchaPNG)
	writeJSON(w, http.StatusOK, map[string]any{
		"code": 1,
		"msg":  "success",
		"data": map[string]any{"b64s": dataURL, "id": id},
	})
}

func (s *server) handleSendSMS(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"code": -1, "msg": "method not allowed"})
		return
	}
	var req struct {
		Mobile string `json:"mobile"`
		Code   string `json:"code"`
		ID     string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"code": -1, "msg": "请求格式错误"})
		return
	}
	log.Printf("  send-sms mobile=%s imageCode=%q captchaID=%s -> accepted (demo SMS code %s)",
		req.Mobile, req.Code, req.ID, demoSMSCode)
	writeJSON(w, http.StatusOK, map[string]any{"code": 1, "msg": "success"})
}

func (s *server) handleEmailLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"code": -1, "msg": "method not allowed"})
		return
	}
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
		DeviceID string `json:"deviceId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"code": -1, "msg": "请求格式错误"})
		return
	}
	if !strings.EqualFold(req.Email, demoEmail) || req.Password != demoPassword {
		writeJSON(w, http.StatusOK, map[string]any{"code": -1, "msg": "密码错误"})
		return
	}
	token := "mock-" + randomHex(8) + "-email"
	s.addToken(token, req.Email)
	log.Printf("  email-login ok deviceId=%s", req.DeviceID)
	writeJSON(w, http.StatusOK, map[string]any{
		"code": 1, "msg": "success",
		"data": map[string]any{"token": token},
	})
}

func (s *server) handleSMSLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"code": -1, "msg": "method not allowed"})
		return
	}
	var req struct {
		Mobile   string `json:"mobile"`
		Captcha  string `json:"captcha"`
		DeviceID string `json:"deviceId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"code": -1, "msg": "请求格式错误"})
		return
	}
	if req.Captcha != demoSMSCode {
		writeJSON(w, http.StatusOK, map[string]any{"code": -1, "msg": "验证码错误"})
		return
	}
	token := "mock-" + randomHex(8) + "-sms"
	s.addToken(token, req.Mobile)
	log.Printf("  sms-login ok mobile=%s deviceId=%s", req.Mobile, req.DeviceID)
	writeJSON(w, http.StatusOK, map[string]any{
		"code": 1, "msg": "success",
		"data": map[string]any{"token": token},
	})
}

func (s *server) handleUserInfo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"code": -1, "msg": "method not allowed"})
		return
	}
	token := r.Header.Get("token")
	account, ok := s.lookupToken(token)
	if !ok {
		// The real API returns JSON when no token header is present.
		writeJSON(w, http.StatusOK, map[string]any{"code": -101, "msg": "未登录"})
		return
	}
	w.Header().Set("Content-Type", "application/x-protobuf")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(userInfoProto(account))
}

func (s *server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"code": -1, "msg": "method not allowed"})
		return
	}
	token := r.Header.Get("token")
	s.removeToken(token)
	log.Printf("  logout token=%s", shortToken(token))
	writeJSON(w, http.StatusOK, map[string]any{"code": 1, "msg": "success"})
}

// ---------------------------------------------------------------------------
// state helpers

func (s *server) addToken(token, account string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tokens[token] = account
}

func (s *server) lookupToken(token string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	account, ok := s.tokens[token]
	return account, ok
}

func (s *server) removeToken(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.tokens, token)
}

// ---------------------------------------------------------------------------
// protobuf encoding for the /v1/user/info response

func userInfoProto(account string) []byte {
	phone := ""
	if !strings.Contains(account, "@") {
		phone = account
	} else {
		phone = "13800001234"
	}
	email := ""
	if strings.Contains(account, "@") {
		email = account
	} else {
		email = "demo@yunhu.chat"
	}

	// Status{code=1, msg="success"}
	var status []byte
	status = protowire.AppendTag(status, 2, protowire.VarintType)
	status = protowire.AppendVarint(status, 1)
	status = appendString(status, 3, "success")

	// UserInfo.Data
	var data []byte
	data = appendString(data, 1, demoUserID)
	data = appendString(data, 2, demoName)
	data = appendString(data, 4, "https://chat-img.jwznb.com/demo.png")
	data = appendVarintField(data, 5, 20240101)
	data = appendString(data, 6, phone)
	data = appendString(data, 7, email)
	data = protowire.AppendTag(data, 8, protowire.Fixed64Type)
	data = protowire.AppendFixed64(data, math.Float64bits(123.45)) // coin
	data = appendVarintField(data, 9, 1)                           // is_vip
	data = appendVarintField(data, 10, uint64(time.Now().AddDate(1, 0, 0).Unix()))
	data = appendVarintField(data, 11, 1) // vip_status=active
	data = appendString(data, 12, "YH-DEMO-0001")

	var out []byte
	out = protowire.AppendTag(out, 1, protowire.BytesType)
	out = protowire.AppendBytes(out, status)
	out = protowire.AppendTag(out, 2, protowire.BytesType)
	out = protowire.AppendBytes(out, data)
	return out
}

func appendString(b []byte, field protowire.Number, value string) []byte {
	b = protowire.AppendTag(b, field, protowire.BytesType)
	return protowire.AppendString(b, value)
}

func appendVarintField(b []byte, field protowire.Number, value uint64) []byte {
	b = protowire.AppendTag(b, field, protowire.VarintType)
	return protowire.AppendVarint(b, value)
}

// ---------------------------------------------------------------------------
// misc helpers

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func randomHex(n int) string {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf)
}

func shortToken(token string) string {
	if len(token) > 12 {
		return token[:12] + "…"
	}
	return token
}
