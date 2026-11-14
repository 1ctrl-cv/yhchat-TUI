package tui

import (
	"image"

	"yhchat-tui/internal/api"
)

// Internal Bubble Tea messages exchanged between the screens and the root App.

// autoLoginMsg is produced by the startup token check.
type autoLoginMsg struct {
	user *api.UserInfo
	err  error
}

// captchaLoadedMsg is produced after fetching an image captcha.
type captchaLoadedMsg struct {
	captcha *api.ImageCaptcha
	img     image.Image // decoded captcha image, nil on error
	err     error
}

// smsSentMsg is produced after asking the server to send an SMS code.
type smsSentMsg struct{ err error }

// loginSucceededMsg carries a fresh token and (optionally) the account profile.
type loginSucceededMsg struct {
	token   string
	user    *api.UserInfo
	userErr error
	method  string // "email" or "sms"
	account string // email or mobile, remembered for next run
}

// loginFailedMsg reports a failed login attempt.
type loginFailedMsg struct{ err error }

// logoutDoneMsg is produced after the logout request finished (or was skipped).
type logoutDoneMsg struct{ err error }

// homeUserMsg refreshes the account profile on the home screen.
type homeUserMsg struct {
	user *api.UserInfo
	err  error
}

// expiringTokenMsg asks the App to drop the token and return to the login
// screen because the server rejected it.
type expiringTokenMsg struct{ err error }

// ---------------------------------------------------------------------------
// Main window (chat) messages

// conversationsLoadedMsg carries a refreshed conversation list.
type conversationsLoadedMsg struct {
	conversations []api.Conversation
	err           error
}

// messagesLoadedMsg carries the history of one conversation.
type messagesLoadedMsg struct {
	chatID   string
	chatType api.ChatType
	messages []api.Message
	err      error
}

// messageSentMsg reports the result of sending a text message.
type messageSentMsg struct {
	chatID string
	msgID  string
	text   string
	err    error
}

// chatUserMsg carries the account profile needed to mark own messages.
type chatUserMsg struct {
	user *api.UserInfo
	err  error
}

// wsEventMsg carries one realtime WebSocket event (or the closed signal).
type wsEventMsg struct {
	event  api.WSEvent
	closed bool
}

// olderMessagesLoadedMsg carries an older page of message history.
type olderMessagesLoadedMsg struct {
	chatID   string
	messages []api.Message
	err      error
}

// messageRecalledMsg reports a recall request result.
type messageRecalledMsg struct {
	chatID string
	msgIDs []string
	err    error
}

// notificationsDismissedMsg reports a mark-as-read request result.
type notificationsDismissedMsg struct {
	chatID string
	err    error
}

// openContactChatMsg asks the App to open a chat with a contact/group/bot.
type openContactChatMsg struct {
	chatID    string
	chatType  api.ChatType
	name      string
	avatarURL string
}

// openHomeMsg asks the App to show the account ("我的") screen.
type openHomeMsg struct{}

// openChatMsg asks the App to show the main chat window.
type openChatMsg struct{}
