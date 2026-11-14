package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestEditNickname(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/user/edit-nickname" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("token") != "tok-1" {
			t.Errorf("token header = %q", r.Header.Get("token"))
		}
		body, _ := io.ReadAll(r.Body)
		if got := string(parseRequestFieldBytes(t, body, 3)); got != "新昵称" {
			t.Errorf("name = %q", got)
		}
		w.Header().Set("Content-Type", "application/x-protobuf")
		_, _ = w.Write(testStatusResponse(1, "success"))
	}))
	defer srv.Close()

	client := newTestClient(t)
	client.BaseURL = srv.URL
	if err := client.EditNickname(context.Background(), "tok-1", "  新昵称  "); err != nil {
		t.Fatalf("EditNickname: %v", err)
	}
}

func TestEditNicknameValidation(t *testing.T) {
	client := newTestClient(t)
	if err := client.EditNickname(context.Background(), "tok", "   "); err == nil {
		t.Fatal("expected error for empty nickname")
	}
}

func TestEditNicknameSessionExpired(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-protobuf")
		_, _ = w.Write(testStatusResponse(2, "未登录，请登录"))
	}))
	defer srv.Close()

	client := newTestClient(t)
	client.BaseURL = srv.URL
	err := client.EditNickname(context.Background(), "bad", "x")
	if err == nil || !IsSessionExpired(err) {
		t.Fatalf("expected session expired error, got %v", err)
	}
}

func TestEditAvatar(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/user/edit-avatar" {
			t.Errorf("path = %s", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		if got := string(parseRequestFieldBytes(t, body, 2)); got != "https://example.com/a.png" {
			t.Errorf("url = %q", got)
		}
		w.Header().Set("Content-Type", "application/x-protobuf")
		_, _ = w.Write(testStatusResponse(1, "success"))
	}))
	defer srv.Close()

	client := newTestClient(t)
	client.BaseURL = srv.URL
	if err := client.EditAvatar(context.Background(), "tok-1", "https://example.com/a.png"); err != nil {
		t.Fatalf("EditAvatar: %v", err)
	}
	if err := client.EditAvatar(context.Background(), "tok-1", "not-a-url"); err == nil {
		t.Fatal("expected validation error for bad avatar url")
	}
}

func TestUserData(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/user/get-user-data" {
			t.Errorf("path = %s", r.URL.Path)
		}
		writeJSONResp(w, 200, `{"code":1,"msg":"success","data":{"data":{
			"id":7,"userId":"100086","lastLoginTime":1710000000,"update_time":1710000100,
			"introduction":"hello","gender":1,"birthday":946656000,
			"province":"北京市","city":"北京城区","district":"东城区","locationCode":"110101"
		}}}`)
	}))
	defer srv.Close()

	client := newTestClient(t)
	client.BaseURL = srv.URL
	data, err := client.UserData(context.Background(), "tok-1")
	if err != nil {
		t.Fatalf("UserData: %v", err)
	}
	if data.UserID != "100086" || data.Introduction != "hello" || data.Gender != 1 ||
		data.Province != "北京市" || data.LocationCode != "110101" || data.UpdateTime != 1710000100 {
		t.Fatalf("unexpected user data: %+v", data)
	}
}

func TestUserDataMissing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSONResp(w, 200, `{"code":1,"msg":"success","data":{}}`)
	}))
	defer srv.Close()

	client := newTestClient(t)
	client.BaseURL = srv.URL
	if _, err := client.UserData(context.Background(), "tok-1"); err == nil {
		t.Fatal("expected error for empty user data")
	}
}

func TestAddFriend(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/friend/apply" {
			t.Errorf("path = %s", r.URL.Path)
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode body: %v", err)
		}
		if payload["chatId"] != "2002" {
			t.Errorf("chatId = %v", payload["chatId"])
		}
		if payload["chatType"] != float64(2) {
			t.Errorf("chatType = %v", payload["chatType"])
		}
		if payload["remark"] != "你好" {
			t.Errorf("remark = %v", payload["remark"])
		}
		writeJSONResp(w, 200, `{"code":1,"msg":"success"}`)
	}))
	defer srv.Close()

	client := newTestClient(t)
	client.BaseURL = srv.URL
	if err := client.AddFriend(context.Background(), "tok-1", "2002", ChatTypeGroup, "你好"); err != nil {
		t.Fatalf("AddFriend: %v", err)
	}
	if err := client.AddFriend(context.Background(), "tok-1", "1", ChatType(9), ""); err == nil {
		t.Fatal("expected validation error for invalid chat type")
	}
}

func TestAddFriendAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSONResp(w, 200, `{"code":-9,"msg":"您已添加过对方"}`)
	}))
	defer srv.Close()

	client := newTestClient(t)
	client.BaseURL = srv.URL
	err := client.AddFriend(context.Background(), "tok-1", "2002", ChatTypeGroup, "hi")
	if err == nil || !strings.Contains(err.Error(), "您已添加过对方") {
		t.Fatalf("err = %v", err)
	}
}
